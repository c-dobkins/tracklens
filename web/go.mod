// This file makes web/ a separate Go module so `go build ./...` and
// `go vet ./...` in the main module skip node_modules, which can contain
// stray .go files. There is no Go code here.
module github.com/c-dobkins/p1/web

go 1.26
