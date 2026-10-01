# The README demo: a YAML file becomes a lazygit-style app.
# Render from the repo root:  vhs docs/demo/demo.tape   (needs vhs, go, git, bat)

Output docs/demo/demo.gif

Require go
Require git
Require bat

Set Shell bash
Set FontSize 16
Set Width 1400
Set Height 820
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms
Set CursorBlink false

# Setup, off camera: build lazify, make a small repo, show YAML highlighted.
Hide
Type 'export DEMO=$(mktemp -d) && go build -o $DEMO/bin/lazify ./cmd/lazify && docs/demo/setup.sh $DEMO/weather && export PATH=$DEMO/bin:$PATH && cd $DEMO/weather'
Enter
Type 'cat() { bat --paging=never --style=plain --theme=ansi "$@"; } && clear'
Enter
Sleep 3s
Show

# 1. The whole app.
Type "cat app.yaml"
Sleep 400ms
Enter
Sleep 4s

# 2. Run it.
Type "lazify app.yaml"
Sleep 400ms
Enter
Sleep 2s

# Branches drive everything: the log follows the cursor.
Down
Sleep 1.2s
Down
Sleep 1.2s
Up
Sleep 1.2s
Up
Sleep 1.5s

# Commits follow the branch; the diff follows the commit.
Tab
Sleep 1.5s
Down
Sleep 1.5s
Down
Sleep 1.5s
Type "J"
Sleep 300ms
Type "J"
Sleep 300ms
Type "J"
Sleep 1.5s

Type "q"
Sleep 800ms

# 3. The punchline.
Type "wc -l app.yaml"
Sleep 400ms
Enter
Sleep 4s
