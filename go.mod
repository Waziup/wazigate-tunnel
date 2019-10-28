module github.com/Waziup/waziup-tunnel

go 1.12

require (
	github.com/Waziup/waziup-tunnel/mqtt v0.0.0-00010101000000-000000000000
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45
)

replace github.com/Waziup/waziup-tunnel/mqtt => ./mqtt
