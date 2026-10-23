package server

// codeCommit is the git commit the binary was built from. It is stamped at
// build time with
//
//	-ldflags -X github.com/Wayfare-labs/wayfare/server.codeCommit=$(git rev-parse HEAD)
//
// and is empty for unstamped (dev/test) builds.
var codeCommit string

// dataCommit is the git commit of data/ that was embedded in the binary at
// build time. It is stamped at build time with
//
//	-ldflags -X github.com/Wayfare-labs/wayfare/server.dataCommit=$(git log -1 --format=%H -- data/)
//
// and is empty for unstamped (dev/test) builds.
var dataCommit string
