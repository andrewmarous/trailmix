# Investigate ramp parity

Code inspection found a plausible rounding mechanism in collision clipping.
A tiny inward residual can trigger a minimum outward correction of 0.03125 units per second.
This can amplify a small arithmetic difference.

The root cause remains unconfirmed at this point.
Removing the correction can restore inward drift or stuck states.
Record a repeatable Utopia or Kitsune segment and compare the first differing collision decision.

## Evidence

This is the early code-inspection hypothesis, before the real-map recording and targeted fix.
It is not a verified diagnosis of a later regression.
