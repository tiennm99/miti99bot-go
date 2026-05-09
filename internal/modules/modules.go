package modules

// This file used to hold a static `Factories` catalog. With concrete modules
// now living in subpackages (internal/modules/util, /misc, …), keeping the
// catalog here would create an import cycle (modules → util → modules).
//
// The composition root in cmd/server owns the catalog instead. Tests pass
// their own catalog into Build, exercising only the modules they care about.
