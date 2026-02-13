Read a file (with optional line range) or list a directory.

Inputs:
- path (required): File or directory path. Relative paths are resolved against the workspace root.
- read_range (optional): Line range `[start, end]` (1-indexed, inclusive).

Behavior & limits:
- If path is a directory, returns a simple entry listing (directories end with `/`).
- If path is a file and read_range is omitted, returns the first 500 lines.
- Output format is:
  - Header: `<path> (<total> lines, <ext> file)`
  - Body: `N: <line>`

Examples:
- {"path":"./config.yml"}
- {"path":"./internal/server/server.go","read_range":[1,120]}
