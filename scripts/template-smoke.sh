#!/usr/bin/env bash

# Exercise the repository as a freshly-created GitHub template repository.
#
# The source is archived from Git (rather than copied from the working tree) so
# this check cannot accidentally pass because of ignored files, a local .env,
# or a developer's Git metadata.  The generated checkout is disposable and is
# intentionally committed once to prove that bootstrap leaves a normal,
# reviewable downstream repository behind.
set -Eeuo pipefail

IFS=$'\n\t'

readonly new_module="github.com/acme/orders-api"
readonly new_slug="orders-api"
readonly new_owner="acme"
readonly new_author="Acme Engineering"
readonly new_email="engineering@acme.example"

fail() {
	echo "template-smoke: $*" >&2
	exit 1
}

log() {
	echo "template-smoke: $*"
}

title_from_slug() {
	local slug="$1"
	local part first rest result=""
	local IFS='-_.'
	read -r -a parts <<< "${slug}"
	for part in "${parts[@]}"; do
		[[ -n "${part}" ]] || continue
		first="$(printf '%s' "${part:0:1}" | tr '[:lower:]' '[:upper:]')"
		rest="${part:1}"
		result+=" ${first}${rest}"
	done
	printf '%s' "${result# }"
}

show_source_file() {
	local path="$1"
	git -C "${root}" show "${source_commit}:${path}"
}

assert_file_contains() {
	local path="$1"
	local token="$2"
	if ! grep -Fq -- "${token}" "${path}"; then
		fail "${path#"${downstream}"/} does not contain expected token: ${token}"
	fi
}

assert_template_token_absent() {
	local label="$1"
	local token="$2"
	local matches
	local status
	[[ -z "${token}" ]] && return 0

	if matches="$(git -C "${downstream}" grep -I -n -F -- "${token}" 2>/dev/null)"; then
		printf '%s\n' "${matches}" >&2
		fail "template ${label} remains in the generated checkout (${token})"
	else
		status=$?
		if [[ "${status}" -ne 1 ]]; then
			fail "could not scan generated checkout for ${label} (git grep exited ${status})"
		fi
	fi
}

root="$(git rev-parse --show-toplevel 2>/dev/null)" || fail "run from inside a Git worktree"
source_ref="${TEMPLATE_SOURCE_REF:-HEAD}"
source_commit="$(git -C "${root}" rev-parse --verify --end-of-options "${source_ref}^{commit}" 2>/dev/null)" || \
	fail "source ref '${source_ref}' is not a commit"
[[ "${source_commit}" =~ ^[0-9a-fA-F]{40,64}$ ]] || fail "Git returned an invalid source commit"

for required in go.mod README.md LICENSE cmd/bootstrap/main.go; do
	git -C "${root}" cat-file -e "${source_commit}:${required}" 2>/dev/null || \
		fail "source ref '${source_ref}' is missing ${required}"
done

# Read identity from the source snapshot instead of embedding this template's
# values in the script.  That keeps the smoke check useful after bootstrap also
# rewrites this file in a downstream checkout.
old_module="$(show_source_file go.mod | awk '$1 == "module" { print $2; exit }')"
[[ -n "${old_module}" ]] || fail "could not determine source module path"
old_slug="${old_module##*/}"
if [[ "${old_slug}" =~ ^v[0-9]+$ && "${old_module}" == */* ]]; then
	old_module_without_version="${old_module%/*}"
	old_slug="${old_module_without_version##*/}"
fi
old_owner=""
if [[ "${old_module}" == github.com/*/* ]]; then
	old_owner="${old_module#github.com/}"
	old_owner="${old_owner%%/*}"
fi
old_title="$(show_source_file README.md | awk '/^# / { sub(/^# /, ""); print; exit }')"
old_author="$(show_source_file LICENSE | sed -n -E 's/^Copyright \(c\) [0-9-]+[[:space:]]+//p' | awk 'NR == 1 { print; exit }')"
new_title="$(title_from_slug "${new_slug}")"

# Policy documents are the only files from which bootstrap intentionally
# discovers maintainer addresses.  Keep the same scope here and fail if any
# source address survives in the generated repository.
old_emails="$({
	for policy in SECURITY.md CODE_OF_CONDUCT.md; do
		if git -C "${root}" cat-file -e "${source_commit}:${policy}" 2>/dev/null; then
			show_source_file "${policy}"
		fi
	done
} | grep -Eo '[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}' | sort -u || true)"

temporary_root="$(mktemp -d "${TMPDIR:-/tmp}/template-smoke.XXXXXX")"
if [[ "${KEEP_TEMPLATE_SMOKE:-0}" == "1" ]]; then
		cleanup() { log "keeping disposable checkout at ${temporary_root}"; }
else
		cleanup() { rm -rf -- "${temporary_root}"; }
fi
trap cleanup EXIT

archive="${temporary_root}/template.tar"
downstream="${temporary_root}/downstream"
mkdir -p "${downstream}"

log "archiving ${source_ref} from ${root}"
git -C "${root}" archive --format=tar --output="${archive}" "${source_commit}" -- || \
	fail "could not archive source ref '${source_ref}'"
tar -xf "${archive}" -C "${downstream}"

log "initializing disposable downstream Git repository"
git -C "${downstream}" init --quiet --initial-branch=main
git -C "${downstream}" config user.name "Template Smoke"
git -C "${downstream}" config user.email "template-smoke@example.invalid"
git -C "${downstream}" add --all
git -C "${downstream}" commit --quiet --message "template snapshot"
[[ -z "$(git -C "${downstream}" status --porcelain=v1 --untracked-files=all)" ]] || \
	fail "fresh downstream snapshot is not clean"

log "running bootstrap customization"
(
	cd "${downstream}"
	go run ./cmd/bootstrap \
		--module "${new_module}" \
		--slug "${new_slug}" \
		--owner "${new_owner}" \
		--author "${new_author}" \
		--email "${new_email}"
)

module="$(sed -n -E 's/^module[[:space:]]+([^[:space:]]+).*/\1/p' "${downstream}/go.mod" | awk 'NR == 1 { print; exit }')"
[[ "${module}" == "${new_module}" ]] || \
	fail "generated go.mod module is '${module}', want '${new_module}'"
assert_file_contains "${downstream}/.github/CODEOWNERS" "@${new_owner}"
assert_file_contains "${downstream}/LICENSE" "${new_author}"
assert_file_contains "${downstream}/SECURITY.md" "${new_email}"

# A downstream repository generated from this template can run this same
# workflow.  In that case one or more source values may already equal the
# smoke target (for example, the GitHub owner), so retaining that value is
# expected rather than evidence that bootstrap failed to rewrite it.
if [[ "${old_module}" != "${new_module}" ]]; then
	assert_template_token_absent "module path" "${old_module}"
fi
if [[ "${old_slug}" != "${new_slug}" ]]; then
	assert_template_token_absent "project slug" "${old_slug}"
fi
if [[ "${old_title}" != "${new_title}" ]]; then
	assert_template_token_absent "repository title" "${old_title}"
fi
if [[ "${old_owner}" != "${new_owner}" ]]; then
	assert_template_token_absent "GitHub owner" "${old_owner}"
fi
if [[ "${old_author}" != "${new_author}" ]]; then
	assert_template_token_absent "copyright holder" "${old_author}"
fi
while IFS= read -r old_email; do
	[[ -n "${old_email}" ]] || continue
	[[ "${old_email}" == "${new_email}" ]] && continue
	assert_template_token_absent "maintainer email" "${old_email}"
done <<< "${old_emails}"

changed_files="$(git -C "${downstream}" diff --name-only)"
if [[ -n "${changed_files}" ]]; then
	grep -qxF 'go.mod' <<< "${changed_files}" || fail "bootstrap did not update go.mod"
	grep -qxF 'README.md' <<< "${changed_files}" || fail "bootstrap did not update README.md"
else
	log "bootstrap target already matches this checkout"
fi
[[ -z "$(git -C "${downstream}" ls-files --others --exclude-standard)" ]] || \
	fail "bootstrap created unexpected untracked files"
git -C "${downstream}" diff --check

log "testing generated downstream module"
(
	cd "${downstream}"
	go mod tidy -diff
	go mod verify
	go vet ./...
	go test -race -count=1 ./...
	go build ./...
)

log "committing and checking generated downstream diff"
if [[ -n "${changed_files}" ]]; then
	git -C "${downstream}" add --all
	git -C "${downstream}" diff --cached --check
	git -C "${downstream}" commit --quiet --message "bootstrap project"
else
	log "downstream checkout already contains the bootstrap target"
fi
[[ -z "$(git -C "${downstream}" status --porcelain=v1 --untracked-files=all)" ]] || \
	fail "generated downstream repository is dirty after its first commit"

log "checking bootstrap idempotence"
second_run="$(
	cd "${downstream}"
	go run ./cmd/bootstrap \
		--module "${new_module}" \
		--slug "${new_slug}" \
		--owner "${new_owner}" \
		--author "${new_author}" \
		--email "${new_email}" \
		--dry-run
)"
grep -qF 'dry-run: 0 file(s) would change' <<< "${second_run}" || \
	fail "bootstrap is not idempotent:\n${second_run}"
git -C "${downstream}" diff --exit-code
[[ -z "$(git -C "${downstream}" status --porcelain=v1 --untracked-files=all)" ]] || \
	fail "idempotence check left the generated repository dirty"

log "downstream template smoke passed"
