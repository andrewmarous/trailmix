package learning

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsolidationActivityBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := boundedOutput{limit: 1024 * 1024}
	w := consolidationActivity{output: &out, cancel: cancel}
	for n := 0; n < 49; n++ {
		if _, err := w.Write([]byte("{\"type\":\"item.st")); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("arted\"}\n")); err != nil {
			t.Fatal(err)
		}
	}
	if ctx.Err() != context.Canceled || !w.exhausted {
		t.Fatalf("activity not stopped: %v %+v", ctx.Err(), w)
	}
}

func TestConsolidationReadGuard(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable; Pi guard runtime check unavailable")
	}
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	if err = os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(bundle, "note.md"), []byte("# Permitted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "outside.md")
	if err = os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(bundle, "escape.md")); err != nil {
		t.Fatal(err)
	}
	guard := filepath.Join(dir, "guard.mjs")
	if err = os.WriteFile(guard, []byte(consolidationGuard), 0600); err != nil {
		t.Fatal(err)
	}
	script := `import { pathToFileURL } from "node:url";
import { readFileSync } from "node:fs";
const hooks = {}; let active; let aborts=0;
const pi={on:(name,fn)=>{hooks[name]=fn},setActiveTools:names=>{active=names}};
(await import(pathToFileURL(process.argv[2]))).default(pi);
const ctx={abort:()=>{aborts++}};
hooks.session_start();
if (JSON.stringify(active)!=='["read","grep","find","ls"]') throw Error("unexpected tool access");
if (readFileSync(process.env.TMIX_CONSOLIDATION_GUARD_STATUS,"utf8")!=="ready") throw Error("guard missing");
if (hooks.tool_call({toolName:"read",input:{path:"note.md"}})?.block) throw Error("valid read blocked");
for(const path of ["../outside.md","escape.md","/etc/passwd"]){
 if (!hooks.tool_call({toolName:"read",input:{path}})?.block) throw Error("escaped bundle: "+path);
}
if(!hooks.tool_call({toolName:"bash",input:{command:"echo unsafe"}})?.block) throw Error("shell enabled");
for(let n=0;n<25;n++) hooks.before_provider_request({},ctx);
if(aborts!==1) throw Error("request limit not enforced");
hooks.tool_result({content:[{type:"text",text:"x".repeat(200)}]},ctx);
if(aborts!==2 || !readFileSync(process.env.TMIX_CONSOLIDATION_GUARD_STATUS,"utf8").includes("tool-read budget")) throw Error("read budget not enforced");
console.log("permitted read; traversal, symlink, shell, turn and read limits enforced");`
	cmd := exec.Command(node, "--input-type=module", "-", guard)
	cmd.Dir = bundle
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(os.Environ(), "TMIX_CONSOLIDATION_GUARD_STATUS="+filepath.Join(dir, "status"), "TMIX_CONSOLIDATION_READ_BYTES=100")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("guard: %s %v", b, err)
	}
	if string(b) != "permitted read; traversal, symlink, shell, turn and read limits enforced\n" {
		t.Fatalf("unexpected guard result: %s", b)
	}
}
