#!/usr/bin/env bash
# Builds a small, deterministic git repo for the demo: setup.sh <dir>
set -euo pipefail
dir=$1
here=$(cd "$(dirname "$0")" && pwd)
rm -rf "$dir" && mkdir -p "$dir" && cd "$dir"
git init -q -b main
git config user.name "Ada Demo" && git config user.email ada@example.com

t=$(( $(date +%s) - 20 * 86400 ))    # first commit 20 days ago, then a day apart
commit() {
  t=$((t + 86400))
  git add -A
  GIT_AUTHOR_DATE="@$t" GIT_COMMITTER_DATE="@$t" git commit -q -m "$1"
}

printf '# weather\n\nA tiny forecast CLI.\n' > README.md
commit "Initial commit"
printf 'package main\n\nfunc main() {\n\tprintln(forecast("Brno"))\n}\n' > main.go
commit "Add main entry point"
printf 'package main\n\nfunc forecast(city string) string {\n\treturn city + ": sunny"\n}\n' > forecast.go
commit "Forecast a city"
printf 'package main\n\nfunc forecast(city string) string {\n\treturn city + ": sunny, 21°C"\n}\n' > forecast.go
commit "Show the temperature"

git checkout -q -b feature/rain
printf 'package main\n\nfunc forecast(city string) string {\n\tif raining(city) {\n\t\treturn city + ": rain, 14°C"\n\t}\n\treturn city + ": sunny, 21°C"\n}\n' > forecast.go
printf 'package main\n\nfunc raining(city string) bool { return city == "London" }\n' > rain.go
commit "Predict rain in London"
printf 'package main\n\nimport "slices"\n\nvar wet = []string{"London", "Bergen"}\n\nfunc raining(city string) bool { return slices.Contains(wet, city) }\n' > rain.go
commit "Bergen is wet too"

git checkout -q main
git checkout -q -b fix/units
sed -i 's/21°C/70°F/' forecast.go
commit "Use Fahrenheit for US users"

git checkout -q main
printf '# weather\n\nA tiny forecast CLI.\n\n    go run . \n' > README.md
commit "Document usage"

cp "$here/app.yaml" .
