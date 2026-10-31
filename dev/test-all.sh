#!/usr/bin/env bash
# test-all runs make test in each go-kanna repository checked out beside this
# one, against the family workspace, dev/go-kanna.work: each module is built
# from the checkout beside it, not from the release its go.mod requires.
#
#	dev/test-all.sh            # internal, the seven generators, and this repository
#	dev/test-all.sh showcase   # the repositories named, here one whose tests need its database
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
work=$root/kanna/dev/go-kanna.work
repos=("$@")
if [ ${#repos[@]} -eq 0 ]; then
	repos=(internal di fixture i18n mapper orm validate value kanna)
fi

failed=()
for r in "${repos[@]}"; do
	if [ ! -d "$root/$r" ]; then
		echo ">>> $r: not checked out, skipped"
		continue
	fi
	echo ">>> $r"
	make -C "$root/$r" test GOWORK="$work" || failed+=("$r")
done

if [ ${#failed[@]} -gt 0 ]; then
	echo "failed: ${failed[*]}"
	exit 1
fi
