module zfs-backup-service

go 1.21

require (
	github.com/bicomsystems/go-libzfs v0.4.0
	github.com/julienschmidt/httprouter v1.3.0
	gopkg.in/yaml.v3 v3.0.1
)

// go-libzfs v0.4.0 does not compile unmodified against modern OpenZFS
// (2.2/2.3); the local copy in third_party/ carries three small documented
// patches (see third_party/go-libzfs/PATCHES.md).
replace github.com/bicomsystems/go-libzfs => ./third_party/go-libzfs
