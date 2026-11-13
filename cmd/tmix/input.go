package main

import (
	"context"
	"io"
	"sync"
)

// commandInput prevents cancellation-induced EOF from publishing partial input.
// It does not close caller-supplied readers; runMain owns CLI input cleanup.
type commandInput struct {
	ctx context.Context
	io.Reader
}

func (r commandInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.Reader.Read(p)
	if canceled := r.ctx.Err(); canceled != nil {
		return 0, canceled
	}
	return n, err
}

// Inherited stdin may use an uninterruptible raw read. A single lazy relay gives
// the CLI an owned pipe. A blocked relay lives only until process exit.
type cliInput struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	source io.Reader
	once   sync.Once
}

func newCLIInput(source io.Reader) *cliInput {
	reader, writer := io.Pipe()
	return &cliInput{reader: reader, writer: writer, source: source}
}

func (r *cliInput) Read(p []byte) (int, error) {
	r.once.Do(func() {
		go func() {
			_, err := io.Copy(r.writer, r.source)
			_ = r.writer.CloseWithError(err)
		}()
	})
	return r.reader.Read(p)
}

func (r *cliInput) Close() {
	_ = r.reader.Close()
	_ = r.writer.Close()
}
