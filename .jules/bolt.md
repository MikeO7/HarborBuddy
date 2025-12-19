## 2024-05-23 - Lazy Logger Initialization
**Learning:** In high-iteration loops where many items might be skipped (e.g., filtering containers), creating a context-rich object (like a logger with fields) *before* the check is wasteful.
**Action:** Always place eligibility checks *before* resource allocation (loggers, heavy structs, API calls). Use the parent logger/object for "skip" or "debug" messages in the negative path.
