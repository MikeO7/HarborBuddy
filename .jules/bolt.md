## 2025-12-16 - Scheduler Time Calculation
**Learning:** The `calculateNextRun` function in `scheduler.go` has a subtle bug when the scheduled time is exactly the same as the current time (down to the minute), but the seconds make it "technically" in the past if `now` has seconds > 0. However, the test failures show "Expected future time today, got next day", which implies `calculateNextRun` is aggressively scheduling for tomorrow even when it shouldn't, or the test's "now" vs the function's "now" are misaligned.

The issue is that `calculateNextRun` constructs a `nextRun` with 0 seconds.
`nextRun := time.Date(..., 0, 0, ...)`
If `now` is `10:00:30` and schedule is `10:00`, `nextRun` (10:00:00) is before `now`. So it schedules for tomorrow. This is technically correct behavior for a scheduler that runs once a day (if you missed the slot, wait for tomorrow).

However, the test `TestCalculateNextRun` constructs "now" with 0 seconds: `now := time.Date(..., 10, 0, 0, 0, locUTC)`.
But `calculateNextRun` calls `time.Now()`. It ignores the `now` passed in the test case struct!
This is why the test is flaky/failing. The test assumes it can control "now" but the function uses `time.Now()`.

**Action:** Refactor `calculateNextRun` to accept a `now` parameter or use an interface for time retrieval to make it testable. Or, for a quick fix in the context of this task, change the test to be robust against "real" time or refactor the code to be testable.

Since I am "Bolt" and I need to be precise, I should probably make `calculateNextRun` pure by accepting `now` as an argument. This improves testability and correctness.
