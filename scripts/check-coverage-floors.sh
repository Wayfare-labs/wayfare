#!/usr/bin/env bash
#
# Fail when coverage of a maintainer-owned package drops below its committed
# floor (#666).
#
# coverage-floors.txt is the policy; this script is the mechanism. Coverage of
# `route`, `dex`, `refrate` and `sep38` is what makes the project's published
# numbers checkable, and a report that nobody enforces decays: the trend is
# always toward "I will add the test next time".
#
# The profile is read directly rather than through `go tool cover -func`,
# because -func reports per function and averaging those percentages would
# weight a ten-statement helper the same as a two-hundred-statement ladder. A
# statement-weighted total is what `go test -cover` prints, and it is what a
# floor should mean.
#
# Usage:
#   scripts/check-coverage-floors.sh [profile] [floors-file]
#
# Exit status: 0 all floors met, 1 a floor was missed or a package was absent
# from the profile, 2 the inputs could not be read.

set -euo pipefail

profile=${1:-coverage.out}
floors=${2:-coverage-floors.txt}
module=github.com/Wayfare-labs/wayfare

if [[ ! -f "$profile" ]]; then
	printf 'coverage-floors: no coverage profile at %s\n' "$profile" >&2
	printf 'coverage-floors: run: go test -coverprofile=%s ./route/... ./dex/... ./refrate/... ./sep38/...\n' "$profile" >&2
	exit 2
fi

if [[ ! -f "$floors" ]]; then
	printf 'coverage-floors: no floors file at %s\n' "$floors" >&2
	exit 2
fi

if ! grep -q '^mode:' "$profile"; then
	printf 'coverage-floors: %s is not a Go coverage profile (no mode line)\n' "$profile" >&2
	exit 2
fi

# Statement-weighted coverage per package, then the comparison. Reading the
# floors first (FNR == NR) keeps the policy file's order, so the table reads
# in the same order as the file a maintainer edits.
awk -v module="$module" -v floors_file="$floors" '
	FNR == NR {
		line = $0
		sub(/#.*/, "", line)
		if (line ~ /^[[:space:]]*$/) next
		n = split(line, fields, /[[:space:]]+/)
		pkg = fields[1]
		floor[pkg] = fields[2] + 0
		order[++nf] = pkg
		next
	}

	/^mode:/ { next }

	{
		# <file>:<start>,<end> <statements> <count>
		file = $1
		sub(/:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/, "", file)
		sub("^" module "/", "", file)

		# The package is the directory holding the file. Root-level files
		# belong to the module package itself, spelled "." here.
		pkg = file
		if (index(pkg, "/") > 0) {
			sub(/\/[^\/]*$/, "", pkg)
		} else {
			pkg = "."
		}

		stmts = $2 + 0
		total[pkg] += stmts
		if ($3 + 0 > 0) covered[pkg] += stmts
		seen[pkg] = 1
	}

	END {
		status = 0
		printf "%-10s %7s %7s   %s\n", "package", "floor", "actual", "status"

		for (i = 1; i <= nf; i++) {
			pkg = order[i]
			if (!(pkg in total) || total[pkg] == 0) {
				printf "%-10s %6.1f%% %7s   MISSING FROM PROFILE\n", pkg, floor[pkg], "-"
				status = 1
				continue
			}
			actual = 100 * covered[pkg] / total[pkg]
			verdict = "ok"
			if (actual < floor[pkg]) {
				verdict = "BELOW FLOOR"
				status = 1
			}
			printf "%-10s %6.1f%% %6.1f%%   %s\n", pkg, floor[pkg], actual, verdict
		}

		if (status != 0) {
			printf "\nCoverage floors are in %s. If the drop is intentional, " \
				"lower the floor in the same pull request and say why; if it is " \
				"not, add the test that restores it. See docs/coverage-floors.md.\n", floors_file
		}
		exit status
	}
' "$floors" "$profile"
