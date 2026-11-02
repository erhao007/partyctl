package version

// version is the partyctl build version. Release builds override it with
// -ldflags "-X github.com/partyctl/partyctl/internal/version.version=<tag>".
var version = "dev"

// String returns the link-time version, or "dev" for local builds.
func String() string {
	return version
}
