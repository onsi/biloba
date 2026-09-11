# release-npm-wait.sh: wait until npm's registry actually serves a set of packages.
#
# Sourced by release.sh so `wait_for_npm_packages` can also be sourced and driven directly in a
# test (with npm shimmed on PATH and the timeout/interval env vars shortened).
#
# A publish isn't necessarily live right away: npm can hold a new package server-side ("your
# package is being processed and may take a few minutes to become available") on top of the usual
# packument lag, and biloba's exact-version optionalDependencies need to resolve the moment biloba
# itself is visible or `npm install` silently skips them.  This polls
# `npm view name@version version --prefer-online` for each pending package until it reports the
# version, printing progress about once a minute, or fails loudly naming whatever is still missing.
#
# Requires `fail` (see release.sh) to be defined by the caller.

npm_wait_timeout_secs=${BILOBA_RELEASE_NPM_WAIT_TIMEOUT_SECS:-1800}
npm_wait_interval_secs=${BILOBA_RELEASE_NPM_WAIT_INTERVAL_SECS:-15}

wait_for_npm_packages() {
	local version=$1
	shift
	local pending=("$@")
	local start=$SECONDS deadline=$((SECONDS + npm_wait_timeout_secs)) last_progress=$SECONDS
	while [[ ${#pending[@]} -gt 0 && $SECONDS -lt $deadline ]]; do
		local remaining=()
		for package in "${pending[@]}"; do
			[[ "$(npm view "$package@$version" version --prefer-online 2>/dev/null)" == "$version" ]] || remaining+=("$package")
		done
		pending=("${remaining[@]}")
		[[ ${#pending[@]} -eq 0 ]] && break
		if ((SECONDS - last_progress >= 60)); then
			printf 'still waiting on npm after %ds: %s\n' "$((SECONDS - start))" "${pending[*]}"
			last_progress=$SECONDS
		fi
		sleep "$npm_wait_interval_secs"
	done
	[[ ${#pending[@]} -eq 0 ]] || fail "npm never served ${pending[*]}@$version - npm may still be processing them; re-run the release to resume (published packages are skipped, this wait runs again, then biloba publishes)"
}
