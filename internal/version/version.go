package version

var (
	Version = "0.2.10-dev"
	Commit  = "unknown"
	Date    = "unknown"
)

type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

func Current() Info {
	return Info{Name: "jikim", Version: Version, Commit: Commit, Date: Date}
}
