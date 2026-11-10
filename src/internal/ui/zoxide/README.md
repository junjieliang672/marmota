# zoxide package

Asks zoxide for folders (`Query`) and keeps only those still on disk, for the
Folders section of the s popup (`src/internal/ui/jump`). The existence check
runs through a worker pool and a short-lived cache (`pathcache.go`), so a slow
mount doesn't stall typing.
