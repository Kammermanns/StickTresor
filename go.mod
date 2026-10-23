module github.com/Kammermanns/StickTresor

go 1.24.0

toolchain go1.24.7

require (
	github.com/jchv/go-webview2 v0.0.0-00010101000000-000000000000
	github.com/tobischo/gokeepasslib/v3 v3.6.1
	golang.org/x/sys v0.38.0
)

require (
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/tobischo/argon2 v0.1.0 // indirect
	golang.org/x/crypto v0.45.0 // indirect
)

replace (
	github.com/jchv/go-webview2 => ./third_party/go-webview2
	github.com/jchv/go-winloader => ./third_party/go-winloader
	github.com/tobischo/argon2 => ./third_party/argon2
	github.com/tobischo/gokeepasslib/v3 => ./third_party/gokeepasslib
)
