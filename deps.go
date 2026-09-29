package wayfare

// This file is the executable statement of the dependency policy in
// CONTRIBUTING.md ("Dependencies are shopspring/decimal and BurntSushi/toml")
// and docs/dependency-policy.md: the module runs on exactly two direct
// dependencies, and a third arrives only by replacing one of them.
//
// Why the position is defended in code rather than prose: the go.mod line is
// one `go get` away from drifting, and nothing else in the repo notices. The
// test in deps_test.go parses go.mod and go.sum on every build and fails when
// the surface moves, so the drift is caught by CI rather than by a reader who
// happens to diff the module file.
//
// The reasoning this guards is not aesthetic:
//
//   - shopspring/decimal exists because binary floating point cannot represent
//     decimal fractions, and rounding drift in a tool whose purpose is
//     measuring small differences is a correctness bug. It is the only
//     dependency allowed to touch money.
//   - BurntSushi/toml exists because issuers publish identity in stellar.toml
//     per SEP-1; parsing that document with a hand-rolled parser would put
//     asset identity — the thing every measurement depends on — behind code
//     nobody has reviewed.
//   - Everything else is the standard library, on purpose: fewer transitive
//     trees to review, fewer supply-chain surfaces to trust, and a smaller
//     binary to hand a reviewer.

// DirectDependencies is the complete, allowed set of direct module
// dependencies, in go.mod order.
var DirectDependencies = []string{
	"github.com/BurntSushi/toml",
	"github.com/shopspring/decimal",
}

// DirectDependenciesPinned is the exact version each allowed direct
// dependency must resolve to. A dependency whose version moves here has
// moved in go.mod, and go.mod is the pin: there is no vendor directory and
// no other lockfile to drift against.
var DirectDependenciesPinned = map[string]string{
	"github.com/BurntSushi/toml":    "v1.6.0",
	"github.com/shopspring/decimal": "v1.4.0",
}
