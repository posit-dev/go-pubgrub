## What

<!-- What does this change do, and why? -->

## Clean-room attestation

This repository implements the PubGrub algorithm from published prose only. The
independence of that implementation is a property we have to be able to defend,
and it cannot be restored once lost — so every pull request that touches Go code
must carry this attestation.

Tick the box **only if it is true of the work in this pull request**:

- [ ] I attest that I have not read pubgrub-rs/pubgrub, astral-sh/pubgrub, contriboss/pubgrub-go, or any other PubGrub implementation source while writing these changes.

CI fails if the box is unticked on a pull request that changes Go files. If you
cannot tick it truthfully, say so here and stop — do not tick it and explain
afterwards. Someone who has read an implementation can still contribute
documentation, tests derived from published fixtures, and review; they must not
write solver code.

**Sources permitted while implementing:** Weizenbaum's PubGrub article, the Dart
`pub` solver documentation prose, the PubGrub guide's prose pages (not the code
they link to), and published test fixtures such as packse scenarios.

**Gray area:** Swift Package Manager's implementation is Apache-2.0 and legally
safe to read, but is one behavioral spec among several rather than a translation
source. If you consulted it, note that here:

<!-- e.g. "Consulted SwiftPM's error-reporting behavior for X." Leave blank if not. -->

## Testing

<!-- What did you run, and what did it show? -->
