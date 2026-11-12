# Reference: arithmetic correction

## Historical sequence

Early inspection identified a plausible rounding mechanism, not a confirmed root cause.
The later Kitsune recording separated two differences.
At tick 51, trace fractions differed even though clipped velocities still matched.
At tick 61, the reference residual was negative while Wasurf rounded its residual to zero.
The reference therefore applied the outward correction.

The agent reconstructed the recorded arithmetic with specific evaluation orders.
The later reconstruction covered 185 clipping operations, not only the earlier 184 ramp-contact residuals.
Backoff and residual needed different evaluation orders.
A targeted fix then passed the recorded replay.

## Evidence map

- `surf-initial-hypothesis`: uncertainty and the need for real-map evidence.
- `surf-two-divergences`: trace and clipping differences, correction direction, and limits of double precision.
- `surf-exact-reconstruction`: refined arithmetic explanation and remaining binary-inspection uncertainty.
- `surf-replay-result`: command output with 185 exact clips and 300 ticks.
- `surf-completed-fix`: completed targeted fix and explicit coverage limits.

## Grading limits

The tool output reports exact velocities, native/WASM agreement, and rendering-schedule agreement.
Position errors remained nonzero but below the unchanged 0.001 tolerance.
The transcript does not establish the reference binary instruction sequence.
The selected boundary does not establish browser interaction or full-map completion.
The new task supplies no recording, so a historical explanation is not a current diagnosis.
Tick numbers and exact counts support grading but are not required answer wording.

## Memory outcome

Useful learning refines the early hypothesis and retains the direction of the correction mismatch.
It preserves separate arithmetic sites and the limited scope of the successful replay.
A rule such as "remove rounding corrections" contradicts the evidence.
