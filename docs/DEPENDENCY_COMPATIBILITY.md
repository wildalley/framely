# Dependency compatibility review — 2026-11-10

Baseline: `v0.2.2` / `69e3eca`. Candidate branch: `audit/dependency-compat-20261110`.
This branch evaluates the 12 open Dependabot PRs against the native Go/MyGo architecture.

## Decisions

| PR | Upgrade | Decision and required adaptation |
| --- | --- | --- |
| #3 | actions/setup-go 5 → 7.0.0 | Candidate. Node 24 requires runner ≥2.327.1. Preserve the Go cache key based on `go.sum` explicitly. |
| #4 | actions/upload-artifact 4 → 7.0.2 | Candidate. Keep default ZIP archives; test download with the release workflow's pinned download-artifact v4. |
| #5 | astral-sh/setup-uv 6 → 10.3.3 | Candidate. Keep uv 0.11.28 pinned and make the OCR hash lockfiles explicit cache inputs. |
| #6 | actions/checkout 4 → 7.0.1 | Candidate. Existing workflows use push/pull_request/workflow_dispatch/workflow_call, and disable persisted credentials; no unsafe fork checkout opt-in is needed. |
| #7 | x/image 0.46.0 → 0.47.0 | Local tests and four-platform compilation passed. |
| #8 | MyGo 0.7.4 → 0.8.1 | Requires input-cancellation adaptations; the unchanged application fails four new regressions. Candidate includes fixes and tests. |
| #9 | purego 0.11.1 → 0.11.3 | Local tests and four-platform compilation passed. |
| #10 | mpmath 1.3.0 → 1.4.1 | Blocked: pinned SymPy 1.14.0 requires mpmath ≥1.1.0,<1.4. Keep 1.3.0. |
| #11 | godbus/dbus 5.1.0 → 5.2.4 | Local D-Bus, portal, shortcut and race tests passed. Combined resolution selects x/sys 0.49.0. |
| #12 | RapidOCR 3.9.2 → 3.10.0 | Regenerate rapidocr.lock. Packaged offline bilingual, warm-worker and tiled-image integration passed on Linux. Remove the hard-coded old version from the OCR result label. |
| #13 | ONNX Runtime → 1.31.1 on every platform | Blocked as proposed: no Intel macOS wheel. Candidate upgrades Linux, Windows and ARM macOS while retaining Intel macOS 1.23.2; regenerate requirements.lock. |
| #14 | PyInstaller 6.19.0 → 6.22.3 | Regenerate requirements.lock. Complete Linux component build and frozen OCR integration passed. |

All four Python PRs modify source pins without updating the checked-in hash locks; they cannot be merged unchanged. The candidate retains mpmath 1.3.0 and the Intel macOS ONNX Runtime exception.

## Reproduced failures

- Resolving `sympy==1.14.0` with `mpmath==1.4.1` is unsatisfiable.
- Resolving ONNX Runtime 1.31.1 as a binary-only Python 3.12 installation for `x86_64-apple-darwin` is unsatisfiable. Published wheels cover Linux x86_64/aarch64, macOS ARM64 and Windows x86_64/ARM64.
- MyGo 0.8 reports cancelled presses through `InputEvent.Canceled`. Original handlers treated these releases as confirmations: cancelled drawing committed history and discarded redo, cancelled screen selections confirmed, image selections retained unfinished gestures, and OCR clicks changed selection. Four targeted regressions failed before the adaptation and passed afterwards.

The fixes roll back the open annotation transaction while preserving undo/redo, reset cancelled screen drags, restore image selection bounds, and restore only OCR tokens modified by the cancelled gesture. Existing modifier-only key events cannot become a recorded shortcut because the recorder requires a recognized non-modifier key.

## Local validation

- Full Go race suite and static analysis; software-renderer fallback test.
- Cross-compilation of Linux amd64, Windows amd64, macOS arm64 and macOS amd64 with MyGo 0.8.1.
- Repeated image allocation/render/close test: post-GC Go heap 0.90 → 0.96 MiB over 80 measured cycles, three goroutines. This does not measure native GPU allocations.
- All 18 Python build/protocol/release tests passed after lock regeneration.
- Python 3.12 frozen OCR component built from required hashes; `uv pip check` reports all 26 installed distributions compatible.
- Packaged OCR recognized the expected Chinese/English text and emitted progress for cold, warm and tiled requests. One run: 0.51 s cold, 0.08 s warm, 3.42 s tiled; these are smoke-test timings, not a controlled performance benchmark.

## CI validation

Pending four-platform native builds, packaged OCR tests, Arch packaging, and an artifact upload/download round trip on the candidate branch. The round-trip job validates all seven expected release assets without publishing a release.

Native desktop GPU behavior, actual screen-capture permissions, and interaction with physical multi-monitor setups still require manual platform testing; headless tests and cross-compilation do not establish those properties.

## Primary evidence

- MyGo 0.8.0 migration notes: https://github.com/egoist/mygo/releases/tag/v0.8.0
- MyGo 0.8.1 release: https://github.com/egoist/mygo/releases/tag/v0.8.1
- ONNX Runtime wheel metadata: https://pypi.org/pypi/onnxruntime/1.31.1/json
- SymPy dependency constraint: https://pypi.org/pypi/sympy/1.14.0/json
- RapidOCR distribution metadata: https://pypi.org/pypi/rapidocr/3.10.0/json
- PyInstaller distribution metadata: https://pypi.org/pypi/pyinstaller/6.22.3/json
- Exact Actions tags: https://github.com/actions/setup-go/tree/v7.0.0, https://github.com/actions/checkout/tree/v7.0.1, https://github.com/actions/upload-artifact/tree/v7.0.2, https://github.com/astral-sh/setup-uv/tree/v10.3.3

Detailed local logs and upstream metadata are retained under `build/compat-evidence/` in the audit worktree.
