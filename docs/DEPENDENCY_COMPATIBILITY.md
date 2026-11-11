# Dependency compatibility review — 2026-11-10

## 2026-11-11 — MyGo 0.8.3 / Framely 0.3.0

The previous compatibility upgrade to MyGo 0.8.1 succeeded and was released in v0.2.3. The later recording/icon commits still pinned 0.8.1; they did not attempt an upgrade to 0.8.3. This release updates the module and its CLI tool together to the latest stable MyGo 0.8.3, retaining Go 1.27.3.

Upstream reports no breaking Go API changes. The default Markdown mention/reference parsing and generated TypeScript Record/Promise name changes do not affect this native-UI application. The prior InputEvent.Canceled adaptations remain in place. The upgrade includes upstream clip-mask cache budgets, animated mask reuse, idle Windows texture cleanup and Linux overlay behavior fixes; these are upstream changes, not a claim that all Framely GPU paths are leak-free.

Local source race tests, static analysis, all 28 Python regressions and the v0.3.0 version/notes preflight pass with 0.8.3. Complete target-native workers, installers and packaged OCR/media verification run in the tag-triggered release CI. Published assets are accepted only after all seven platform packages and checksums are verified.

Primary source: https://github.com/egoist/mygo/releases/tag/v0.8.3

Baseline: `v0.2.2` / `69e3eca`. Validation branch: `audit/dependency-compat-20261110`. The validated changes are integrated into main for v0.2.3.
This branch evaluates the 12 open Dependabot PRs against the native Go/MyGo architecture.

## Decisions

| PR | Upgrade | Decision and required adaptation |
| --- | --- | --- |
| #3 | actions/setup-go 5 → 7.0.0 | CI passed. Node 24 requires runner ≥2.327.1. Preserve the Go cache key based on `go.sum` explicitly. |
| #4 | actions/upload-artifact 4 → 7.0.2 | CI passed. Keep default ZIP archives; verified download with the release workflow's pinned download-artifact v4. |
| #5 | astral-sh/setup-uv 6 → 10.3.3 | CI passed. Keep uv 0.11.28 pinned and make the OCR hash lockfiles explicit cache inputs. |
| #6 | actions/checkout 4 → 7.0.1 | CI passed. Existing workflows use push/pull_request/workflow_dispatch/workflow_call, and disable persisted credentials; no unsafe fork checkout opt-in is needed. |
| #7 | x/image 0.46.0 → 0.47.0 | Combined race tests, four-platform native builds and packaging passed. |
| #8 | MyGo 0.7.4 → 0.8.1 | Requires input-cancellation adaptations; the unchanged application fails four new regressions. Candidate includes fixes and tests; all four native CI builds and tests passed. |
| #9 | purego 0.11.1 → 0.11.3 | Combined race tests, four-platform native builds and packaging passed. |
| #10 | mpmath 1.3.0 → 1.4.1 | Blocked: pinned SymPy 1.14.0 requires mpmath ≥1.1.0,<1.4. Keep 1.3.0. |
| #11 | godbus/dbus 5.1.0 → 5.2.4 | D-Bus, portal, shortcut and race tests passed locally and in CI. Combined resolution selects x/sys 0.49.0. |
| #12 | RapidOCR 3.9.2 → 3.10.0 | Regenerate rapidocr.lock. Packaged offline bilingual, warm-worker and tiled-image integration passed on Linux, Windows and both macOS architectures. Remove the hard-coded old version from the OCR result label. |
| #13 | ONNX Runtime → 1.31.1 on every platform | Blocked as proposed: no Intel macOS wheel. Candidate upgrades Linux, Windows and ARM macOS while retaining Intel macOS 1.23.2; regenerate requirements.lock. |
| #14 | PyInstaller 6.19.0 → 6.22.3 | Regenerate requirements.lock. Complete component builds and frozen OCR integration passed on all four CI platforms. |

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

First run `42766426201` at `af9ff70`: preflight, full Go checks, Linux/Windows/macOS ARM64/macOS Intel native component builds and packaged OCR tests, and Arch packaging all passed. upload-artifact v7 produced five artifact groups; download-artifact v4 downloaded all five and verified every SHA-256 digest. The final seven-file validation failed because the new audit job omitted `RELEASE_TAG`, not because of an artifact format mismatch. Commit `553a247` derives that parameter from `mygo.json`; the corrected complete run `42767660498` at `553a247139de19445a114779e31d1c9a45d9490d` passed every job:

- preflight;
- full Go race/static/memory checks and cross-compilation;
- Windows amd64 native components, packaged OCR, platform tests and installers;
- Ubuntu 22.04 amd64 native components, capture tests, OCR and Debian/portable packages;
- macOS 15 ARM64 and macOS 15 Intel native components, OCR, platform tests and DMGs;
- Arch native package;
- upload-artifact v7 → download-artifact v4 round trip and validation of all seven expected release files.

Run: https://github.com/wildalley/screenpin/actions/runs/42767660498

The validated combination is included in v0.2.3 with the documented adaptations. Do not merge the original mpmath or all-platform ONNX Runtime PR unchanged. The compatibility audit itself did not replace the user's installed v0.2.2.

Local Arch packaging validation reached the expected bundle validation but could not invoke `dpkg-deb` because the host does not have that command. The native Ubuntu CI packaging job passed, including Debian packaging. No additional system package was installed for this audit.

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
