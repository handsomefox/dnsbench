## Summary

<!-- What was broken, and what is the behavior now? -->

## Verification

- [ ] `make build`
- [ ] `go test -race ./...`
- [ ] `golangci-lint run ./...`
- [ ] `npm run lint --prefix webui`, if the change touches the dashboard

## Notes

<!-- Screenshots for dashboard changes. Say whether the Go structs in server.go
and webui/src/types.ts changed together, since nothing fails to compile when
they drift. Write "none" if there is nothing to add. -->
