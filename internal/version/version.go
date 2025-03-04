package version

var (
	version = "main"
	commit  = "none"
)

func Version() string {
	return version
}

func Commit() string {
	return commit
}
