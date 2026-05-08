package modules

// Factories is the static module catalog. Each phase that introduces a module
// adds an entry here. Phase 03 ships an empty catalog; Phase 05 onwards
// populates it.
//
// Spec deviation: Phase 03 plan defined a `[]Factory` slice. A map keyed by
// module name is required for Build() to honor the MODULES env CSV without a
// linear scan, and prevents duplicate module names at compile-load time.
var Factories = map[string]Factory{}
