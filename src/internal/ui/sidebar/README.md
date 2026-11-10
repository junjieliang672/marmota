# sidebar package

What the sidebar popup used to list, without the popup: the pinned folders
and files (`pinned.go`), the files edited lately (`recent_edits.go`), and the
git status of the focused panel's folder (`git_status.go`). The s list
(`src/internal/ui/jump`) lists the pins and recent files through
`entries.go`; the file rows and the Details box show the git status.
