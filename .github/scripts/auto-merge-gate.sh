#!/usr/bin/env bash
# The auto-merge gate. Run by .github/workflows/auto-merge.yml.
#
# This lives in a file, not inline in the workflow, because GitHub rejects a
# workflow whose single `run:` script exceeds 21,000 characters ("Exceeded max
# expression length 21000"). The whole workflow file is then invalid and every
# trigger silently stops, which is what happened when this grew past the limit.
#
# It is checked out from the BASE branch only; the PR head is never checked
# out, so the no-PR-code rule in the workflow still holds.
set -euo pipefail

# ---------------------------------------------------------------
# Resolve which PR we are looking at. check_suite events carry the
# PR indirectly, so fall back to querying by head SHA.
# ---------------------------------------------------------------
# A CI run triggered by merge_group belongs to a queue entry, not to
# a pull request. Resolving it back to a PR would re-run the gate
# against a commit the queue already owns, so ignore those outright.
if [ "${WORKFLOW_RUN_EVENT:-}" = "merge_group" ]; then
  echo "CI run came from a merge_group entry; nothing to gate"
  exit 0
fi

PR="${EVENT_PR:-}"

# issue_comment fires for issues as well as pull requests, and only
# the latter can be merged. A plain issue carries no
# issue.pull_request.url, so stop here: the pulls endpoint below would
# 404 on it and fail the run for every comment on every issue.
if [ -n "${ISSUE_PR:-}" ] && [ -z "${IS_PR:-}" ]; then
  echo "comment on issue #${ISSUE_PR}, not a pull request; nothing to gate"
  exit 0
fi
if { [ -z "$PR" ] || [ "$PR" = "null" ]; } && [ -n "${ISSUE_PR:-}" ]; then
  PR="${ISSUE_PR}"
fi

if [ -z "$PR" ] || [ "$PR" = "null" ]; then
  # A workflow_run event names only the commit, so resolve the PR
  # from its head SHA.
  PR=$(gh api "repos/$REPO/commits/${HEAD_SHA}/pulls" \
         --jq '.[0].number // empty' 2>/dev/null || true)
fi
if [ -z "$PR" ]; then
  echo "no pull request associated with this event; nothing to do"
  exit 0
fi
echo "evaluating PR #$PR"

# Ignore our own comments, or the gate re-triggers itself in a loop.
COMMENTER="${COMMENT_USER:-}"
if [ "$COMMENTER" = "github-actions[bot]" ]; then
  echo "comment was ours; nothing to re-evaluate"
  exit 0
fi

meta=$(gh api "repos/$REPO/pulls/$PR")
draft=$(jq -r '.draft' <<<"$meta")
state=$(jq -r '.state' <<<"$meta")
author=$(jq -r '.user.login' <<<"$meta")
body=$(jq -r '.body // ""' <<<"$meta")
head_sha=$(jq -r '.head.sha' <<<"$meta")
head_repo=$(jq -r '.head.repo.full_name // ""' <<<"$meta")
readonly_review_event=false
if [ "${GITHUB_EVENT_NAME:-}" = "pull_request_review" ] && [ "$head_repo" != "$REPO" ]; then
  readonly_review_event=true
fi

if [ "$state" != "open" ] || [ "$draft" = "true" ]; then
  echo "PR is $state, draft=$draft; nothing to do"
  exit 0
fi

# Reasons the PR is not auto-mergeable, each paired with what the
# contributor can do about it. Empty means merge.
#
# "waiting" is separate: a verdict is not possible *yet* (CI or
# CodeRabbit has not reported). Waiting alone never comments, but a
# real reason found while waiting is reported straight away, so a
# contributor is not left in silence on a PR that cannot merge.
reasons=()
fixes=()
waiting=()
hold() { reasons+=("$1"); fixes+=("$2"); }

# ---------------------------------------------------------------
# GATE 0 — no malicious code.
#
# The base-branch scanner reads the PR's patches through the API
# (it never runs PR code) and looks for malware and supply-chain
# attack patterns. Its self-tests run first, so a broken or
# weakened scanner holds the PR instead of reporting it clean.
# The "Malware scan" workflow publishes the same verdict as a
# required status; checking here as well means this gate never
# depends on that status having arrived.
# ---------------------------------------------------------------
# Files are fetched through `gh`, so the offline tests' fake gh
# answers them too.
scanner_dir="${GITHUB_WORKSPACE:-.}/.github/scripts"
scan_json="$(mktemp)"
scan_rc=0
if python3 -B "$scanner_dir/test_malware_scan.py" >/dev/null 2>&1; then
  python3 -B "$scanner_dir/malware_scan.py" --gh --repo "$REPO" \
    --pr "$PR" --json > "$scan_json" || scan_rc=$?
else
  scan_rc=3
fi
scan_where() {  # one line: "rule in path:line; ..." for a severity
  # Paths come from the PR, so each is wrapped in a code span with
  # backticks and newlines stripped before it reaches a comment.
  python3 -B -c '
import json, sys
d = [f for f in json.load(open(sys.argv[1])) if f["severity"] == sys.argv[2]]
out = []
for f in d[:5]:
    loc = f["path"] + (":" + str(f["line"]) if f["line"] else "")
    loc = loc.replace("`", "").replace("\n", " ")[:120]
    out.append(f["rule"] + " in `" + loc + "`")
print("; ".join(out) + ("; ..." if len(d) > 5 else ""))
' "$scan_json" "$1" 2>/dev/null || echo "see the job log"
}
case "$scan_rc" in
  0|4) echo "malware scan: ok (rc=$scan_rc)" ;;
  1) hold "contains code matching known malicious patterns ($(scan_where BLOCK)) — do not check this branch out locally" \
          "Remove the flagged code and push again; the scan re-runs on every push. If you believe it is a false positive, explain in a comment what that code does and a maintainer will review it. PRs that fail the scan are never auto-merged and their CI is never auto-approved."

     gh pr edit "$PR" --repo "$REPO" --add-label security-hold >/dev/null 2>&1 || true ;;
  2) hold "touches security-sensitive configuration ($(scan_where HOLD))" \
          "A maintainer reviews changes to this kind of configuration. If it is not essential to your change, move it to its own PR so the rest can merge automatically." ;;
  *) hold "the malware scan could not complete, so the change is not known to be safe" \
          "Nothing for you to fix. A maintainer will re-run or review the scan." ;;
esac

# ---------------------------------------------------------------
# GATE 1 — maintainer-owned paths.
#
# An error in any of these invalidates published measurements rather
# than breaking a feature, so they are never merged unreviewed. This
# is about blast radius, not gatekeeping: individual checks and
# metrics are exactly the contribution the project wants.
# ---------------------------------------------------------------
files=$(gh api --paginate "repos/$REPO/pulls/$PR/files" --jq '.[].filename')
echo "changed files:"; echo "$files" | sed 's/^/  /'

protected='^(route/route\.go|route/ladder\.go|dex/|sep38/|runstore/runstore\.go|checks/engine\.go|\.github/workflows/|data/)'
hits=$(echo "$files" | grep -E "$protected" || true)
if [ -n "$hits" ]; then
  joined=$(echo "$hits" | paste -sd '|' - | sed 's/|/, /g')
  hold "touches maintainer-owned paths: $joined" \
       "These areas are reviewed by a maintainer because an error in them changes published measurements. Nothing is wrong with your change. If the PR also has unrelated changes, split those into a separate PR so they can merge automatically."
fi

# ---------------------------------------------------------------
# APPROVAL — release CI that GitHub is holding for a fork PR.
#
# A first-time contributor's workflows sit in action_required until
# someone approves them, and until then no check runs exist, so the
# gate could only wait. This is the click a maintainer would make.
#
# It is the one place this workflow lets untrusted code run, so it
# is narrow on purpose. Approval happens only when ALL hold:
#   - the malware scan passed (GATE 0 clean, "Malware scan" status
#     success on this exact commit, no security-hold label)
#   - the PR does not edit .github/workflows/
#   - the run is for the PR's current head commit and is a
#     pull_request run (read-only token, no secrets — see ci.yml)
# Approving merges nothing; every gate still has to pass on the
# results. Unknown scan state is not clean: it waits.
# ---------------------------------------------------------------
malware_state=$(gh api "repos/$REPO/commits/$head_sha/status" \
  --jq '[.statuses[] | select(.context == "Malware scan")] | first | .state // "missing"')
secheld=$(jq -r '[(.labels // [])[].name | ascii_downcase] | any(. == "security-hold")' <<<"$meta")
malware_ok=no
if [ "$secheld" = "true" ]; then
  hold "is under a security hold — the malware scan flagged it" \
       "Nothing is merged or run automatically while this label is set. A maintainer will review the scan result."
elif [ "$malware_state" = "success" ]; then
  malware_ok=yes
elif [ "$malware_state" = "failure" ] || [ "$malware_state" = "error" ]; then
  hold "failed the malware scan (\"Malware scan\" status: $malware_state)" \
       "Remove the flagged code and push again; the scan re-runs on every push. If you believe it is a false positive, explain in a comment and a maintainer will review it."
else
  waiting+=("the malware scan has not finished (status: $malware_state)")
fi

if [ "$malware_ok" != "yes" ] || [ "$scan_rc" != 0 -a "$scan_rc" != 4 ]; then
  echo "scan not clean (status=$malware_state, hold=$secheld, rc=$scan_rc); not approving CI runs"
elif echo "$files" | grep -qE '^\.github/workflows/'; then
  echo "PR edits workflows; not approving its CI runs automatically"
else
  run_ids=$(gh api "repos/$REPO/actions/runs?head_sha=$head_sha&status=action_required&per_page=100" \
              --jq '.workflow_runs[] | select(.event=="pull_request") | .id' 2>/dev/null || true)
  for id in $run_ids; do
    if gh api -X POST "repos/$REPO/actions/runs/$id/approve" >/dev/null 2>&1; then
      echo "approved workflow run $id"
    else
      echo "::warning::could not approve workflow run $id"
    fi
  done
fi

# ---------------------------------------------------------------
# GATE 2 — no new dependencies.
#
# The project has two, both pinned. A third is a decision, not a
# detail, and the acceptance criteria forbid adding one.
# ---------------------------------------------------------------
if echo "$files" | grep -qE '^go\.(mod|sum)$'; then
  hold "modifies go.mod or go.sum — new dependencies need review" \
       "The project deliberately has two pinned dependencies. Revert the go.mod/go.sum change if the dependency is not essential, or explain in a comment why it is, and a maintainer will decide."
fi

# (The PR-description checklist is deliberately not a gate. What the
# PR says about itself proves nothing; GATE 7 reads the diff.)

# ---------------------------------------------------------------
# GATE 4 — every check has finished and succeeded.
#
# Neutral and skipped count as success; queued or in-progress means
# we are simply not ready to decide yet.
# ---------------------------------------------------------------
# This job's own check run is excluded. It is necessarily
# in_progress while it evaluates, so counting it would make the gate
# wait for itself forever — every run would report "1 check still
# running" and exit, and nothing would ever merge.
runs=$(gh api --paginate "repos/$REPO/commits/$head_sha/check-runs" \
         | jq --arg self "$SELF_CHECK" -r \
         '.check_runs[] | select(.name != $self)
          | "\(.name)\t\(.status)\t\(.conclusion // "-")\t\(.html_url)"')

# No check runs reported yet is "too early to decide", not "nothing
# to wait for". Treating an empty list as all-clear would merge a PR
# before CI had started on it.
if [ -z "$runs" ]; then
  echo "::notice::no check runs reported yet; will re-evaluate when they appear"
  waiting+=("CI has not reported yet")
else
  echo "check runs:"; echo "$runs" | sed 's/^/  /'

  pending=$(awk -F'\t' '$2!="completed"' <<<"$runs" | wc -l)
  failed=$(awk -F'\t' '$2=="completed" && $3!="success" && $3!="neutral" && $3!="skipped"' <<<"$runs" || true)

  if [ "$pending" -gt 0 ]; then
    echo "::notice::$pending check(s) still running; will re-evaluate when they finish"
    waiting+=("$pending check(s) still running")
  elif [ -n "$failed" ]; then
    names=$(awk -F'\t' '{print $1" ("$3")"}' <<<"$failed" | paste -sd '|' - | sed 's/|/, /g')
    links=$(awk -F'\t' '{print "[" $1 "](" $4 ")"}' <<<"$failed" | paste -sd '|' - | sed 's/|/, /g')
    hold "checks are not green: $names" \
         "Open the failing check(s) and read the log: $links. Reproduce locally with 'make fmt vet test race lint', fix it and push; the gate re-runs on every push. If a failure looks unrelated to your change (a flaky test or an outage), say so in a comment and a maintainer will re-run it."
  fi
fi

# ---------------------------------------------------------------
# GATE 5 — a maintainer has not already asked for changes.
# ---------------------------------------------------------------
changes_requested=$(gh api --paginate "repos/$REPO/pulls/$PR/reviews" \
  --jq '[.[] | select(.state=="CHANGES_REQUESTED")] | length')
if [ "$changes_requested" -gt 0 ]; then
  hold "a reviewer has requested changes" \
       "Address each review comment, push, and reply on the threads. The reviewer has to dismiss or update their review before this can merge automatically; otherwise a maintainer will merge it by hand once satisfied."
fi

# ---------------------------------------------------------------
# GATE 6 — CodeRabbit has actually reviewed this commit.
#
# The merge comment says a CodeRabbit review passed, so that has to
# be true rather than assumed. Claiming a review happened when it did
# not would be exactly the kind of unverified assertion this project
# refuses everywhere else.
#
# Absent is "wait", not "hold": the review may simply not have
# arrived yet, and the pull_request_review and issue_comment triggers
# bring us back when it does.
#
# CodeRabbit posts a formal review on some PRs and a comment on
# others (especially non-Go changes). We try the review first, then
# fall back to the comment. Both carry the same verdict text.
# ---------------------------------------------------------------
# Filter by commit_id so we read the review for *this* commit, not
# an older one left over from a previous push.
rabbit_body=$(gh api --paginate "repos/$REPO/pulls/$PR/reviews" \
  | jq --arg sha "$head_sha" -r \
  '[.[] | select(((.user | type) == "object")
                and ((.user.login // "") | test("coderabbit"; "i"))
                and (.commit_id == $sha))] | last | .body // ""')

# If no formal review, fall back to the most recent CodeRabbit
# comment. CodeRabbit posts a summary comment on every PR; the
# verdict text is the same as in a review.
if [ -z "$rabbit_body" ]; then
  rabbit_body=$(gh api --paginate "repos/$REPO/issues/$PR/comments" \
    | jq -r '[.[] | select(((.user | type) == "object")
                  and ((.user.login // "") | test("coderabbit"; "i")))]
    | last | .body // ""')
fi

if [ -z "$rabbit_body" ]; then
  echo "::notice::no CodeRabbit review yet; will re-evaluate when one arrives"
  waiting+=("CodeRabbit has not reviewed yet")
fi

# CodeRabbit opens its review with "Actionable comments posted: N"
# and its summary comment with "No actionable comments were
# generated". Both carry the verdict: zero means it found nothing it
# wants changed. Configured with request_changes_workflow: false it
# never blocks a merge itself, so this is the signal that carries
# whether the review was actually clean.
actionable=$(grep -oiE 'actionable comments posted:[[:space:]]*[0-9]+' <<<"$rabbit_body" \
  | grep -oE '[0-9]+' | head -1 || true)

# The summary comment uses "No actionable comments" instead of
# "Actionable comments posted: 0". Normalise to 0.
if [ -z "$actionable" ] && grep -qi 'no actionable comments' <<<"$rabbit_body"; then
  actionable=0
fi

# CodeRabbit declines to review some repositories outright (this one has
# fewer than 10 stars) and says so in a comment. That is a standing fact
# about the repository, not a verdict on the PR: it will never post one, so
# holding on it would hold every PR forever. It is treated as "no review
# happened", reported as such, and never claimed as a pass; CI, the scope
# check and the malware scan carry the decision instead.
rabbit_skipped=no
if [ -z "$actionable" ] && grep -qiE 'skip review by coderabbit|does not receive automatic reviews|review skipped' <<<"$rabbit_body"; then
  rabbit_skipped=yes
fi

if [ -z "$rabbit_body" ]; then
  :  # nothing to parse; already recorded as waiting
elif [ "$rabbit_skipped" = yes ]; then
  echo "coderabbit: skipped for this repository; no review to count"
elif [ -z "$actionable" ]; then
  # Format changed, or the review is of a shape we do not recognise.
  # Hold rather than guess: merging on an unparsed review would be
  # claiming it passed without having read it.
  hold "CodeRabbit's review could not be parsed for a verdict, so it is not known to be clean" \
       "Nothing for you to fix. A maintainer will read the review and merge by hand if it is clean."
elif [ "$actionable" -gt 0 ]; then
  hold "CodeRabbit raised $actionable actionable comment(s)" \
       "Open the CodeRabbit review on this PR and resolve each actionable comment: fix the code, or reply on the thread explaining why it does not apply. Push; CodeRabbit re-reviews the new commit and this gate re-runs."
else
  echo "coderabbit: review clean (0 actionable comments)"
fi

# ---------------------------------------------------------------
# GATE 7 — the change is within the scope its issue named.
#
# A shell script cannot judge whether a change is *right*. It can
# check something narrower and still useful: that the PR touched only
# what its issue said it would. The issue template requires a section
# naming the package and file, precisely so this is answerable.
#
# Out-of-scope files are the sprawl that turns a small contribution
# into a review problem, and they are also how an unrelated change
# rides in on an approved one.
# ---------------------------------------------------------------
# The linked issue is found from the PR itself, not only from a
# closing keyword in the description: GitHub's own closing-issue
# links first, then the description, title and branch name, then
# commit messages. What the PR *says* about itself is only used to
# locate the issue; the files actually changed are what get judged.
title=$(jq -r '.title' <<<"$meta")
branch=$(jq -r '.head.ref' <<<"$meta")
issue_num=$(gh api graphql -f query='query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){pullRequest(number:$n){closingIssuesReferences(first:1){nodes{number}}}}}' \
              -f o="${REPO%/*}" -f r="${REPO#*/}" -F n="$PR" \
              --jq '.data.repository.pullRequest.closingIssuesReferences.nodes[0].number // empty' 2>/dev/null || true)
if [ -z "$issue_num" ]; then
  issue_num=$(grep -ioE '(clos(e|es|ed)|fix(e|es|ed)?|resolv(e|es|ed))[[:space:]]+#[0-9]+' <<<"$body $title" \
    | grep -oE '[0-9]+' | head -1 || true)
fi
if [ -z "$issue_num" ]; then
  issue_num=$(grep -oE '#[0-9]+' <<<"$title" | tr -d '#' | head -1 || true)
fi
if [ -z "$issue_num" ]; then
  issue_num=$(grep -oE '(^|[^0-9])[0-9]{2,5}([^0-9]|$)' <<<"$branch" | grep -oE '[0-9]+' | head -1 || true)
fi
if [ -z "$issue_num" ]; then
  issue_num=$(gh api --paginate "repos/$REPO/pulls/$PR/commits" --jq '.[].commit.message' 2>/dev/null \
    | grep -oE '#[0-9]+' | tr -d '#' | head -1 || true)
fi
# A number that is not an issue in this repo (or is another PR) is
# no scope at all; treat it as unfound rather than guessing.
if [ -n "$issue_num" ] && ! gh api "repos/$REPO/issues/$issue_num" --jq 'select(.pull_request | not) | .number' >/dev/null 2>&1; then
  issue_num=""
fi

scope_note=""
if [ -z "$issue_num" ]; then
  hold "could not be linked to an issue, so its intended scope is unknown" \
       "Link the issue this PR resolves: put 'Closes #123' in the description, or use the Development sidebar. The issue says which package and files are in scope, which is what the changed files are checked against."
else
  issue_body=$(gh api "repos/$REPO/issues/$issue_num" --jq '.body // ""' 2>/dev/null || true)
  issue_title=$(gh api "repos/$REPO/issues/$issue_num" --jq '.title // ""' 2>/dev/null || true)

  # Paths the issue names: backticked tokens that look like a file
  # or a package directory.
  named=$(grep -oE '`[a-zA-Z0-9_./-]+`' <<<"$issue_body" \
    | tr -d '`' \
    | grep -E '(/|\.go$|\.md$|\.ya?ml$)' \
    | sed 's:/*$::' | sort -u || true)

  if [ -z "$named" ]; then
    # The issue lists no paths, so there is nothing to compare against. A
    # change confined to documentation and tests cannot alter behaviour or a
    # published figure, so that alone is safe to accept; anything touching
    # code needs a human to confirm it matches what the issue meant.
    nondoc=$(echo "$files" | grep -vE '(^docs/|\.md$|_test\.go$|^testdata/)' || true)
    if [ -z "$nondoc" ]; then
      scope_note="issue #$issue_num names no paths, but every changed file is documentation or tests, which cannot change behaviour"
    else
      hold "issue #$issue_num names no files or packages, so scope cannot be verified mechanically" \
           "Nothing for you to fix. The issue does not list the files or package it covers, so a maintainer will confirm the scope by hand. (Changes limited to docs and tests are accepted without a file list.)"
    fi
  else
    echo "issue #$issue_num names:"; echo "$named" | sed 's/^/    /'
    outside=""
    while IFS= read -r f; do
      [ -z "$f" ] && continue
      # Test files and docs accompanying in-scope work are allowed.
      case "$f" in
        *_test.go|docs/*|*.md) continue ;;
      esac
      match=no
      while IFS= read -r n; do
        [ -z "$n" ] && continue
        case "$f" in
          "$n"|"$n"/*) match=yes; break ;;
        esac
      done <<<"$named"
      [ "$match" = no ] && outside="$outside $f"
    done <<<"$files"

    if [ -n "$outside" ]; then
      hold "changes files outside the scope issue #$issue_num named:$(echo "$outside" | tr ' ' '\n' | sed 's/^/ /' | tr '\n' ' ')" \
           "Move the unrelated changes into their own PR, or, if they are genuinely needed for this fix, explain why in a comment so a maintainer can approve the wider scope. Test files and docs next to in-scope work are always allowed."
    else
      scope_note="every changed file falls within the scope issue #$issue_num named"
    fi
  fi
fi

# ---------------------------------------------------------------
# Decide.
# ---------------------------------------------------------------
if [ "$readonly_review_event" = true ]; then
  echo "::notice::pull_request_review for a fork PR has a read-only token; skipping merge, label, and comment mutations"
  if [ ${#reasons[@]} -gt 0 ]; then
    printf 'not auto-merging:\n'
    printf '  - %s\n' "${reasons[@]}"
  fi
  exit 0
fi

# Still waiting on CI or CodeRabbit and nothing else is wrong: say
# nothing and let the next event bring us back.
if [ ${#reasons[@]} -eq 0 ] && [ ${#waiting[@]} -gt 0 ]; then
  printf 'not deciding yet: %s\n' "${waiting[@]}"
  exit 0
fi

if [ ${#reasons[@]} -eq 0 ]; then
  echo "all gates satisfied; handing off"

  # With a merge queue required on the base branch, a direct merge is
  # refused — the queue is the only way in. --auto enqueues instead,
  # and where no queue is configured it merges as soon as the
  # requirements are met, which this script has just finished
  # verifying. One code path for both, and never two merge paths
  # racing each other for the same pull request.
  #
  # --delete-branch is gone: these are fork branches, and the
  # repository sets delete_branch_on_merge false, so it was asking
  # for something it could not do.
  # Last look before handing off: re-read the scan status and labels
  # so a flag raised since the gate began is never raced, and pin to
  # the evaluated commit so a later push cannot ride in on it.
  final_state=$(gh api "repos/$REPO/commits/$head_sha/status" \
    --jq '[.statuses[] | select(.context == "Malware scan")] | first | .state // "missing"')
  final_hold=$(gh api "repos/$REPO/issues/$PR" \
    --jq '[(.labels // [])[].name | ascii_downcase] | any(. == "security-hold")')
  if [ "$malware_ok" != "yes" ] || [ "$final_state" != "success" ] || [ "$final_hold" != "false" ]; then
    echo "::error::malware check not clean at merge time ($final_state, hold=$final_hold); refusing to merge"
    exit 1
  fi
  if ! gh pr merge "$PR" --repo "$REPO" --squash --auto --match-head-commit "$head_sha"; then
    echo "::error::could not enqueue PR #$PR — confirm auto-merge is enabled on the repository"
    exit 1
  fi
  # Describe what landed, then say plainly what was and was not
  # verified. A merge notice that only says "approved" invites a
  # reader to assume more was checked than actually was.
  stats=$(gh api "repos/$REPO/pulls/$PR" \
    --jq '"\(.changed_files) file(s), +\(.additions) −\(.deletions)"')
  gofiles=$(echo "$files" | grep -cE '\.go$' || true)
  testfiles=$(echo "$files" | grep -cE '_test\.go$' || true)

  {
    echo "**Approved. All CI checks passed and the malware scan is clean.**"
    echo
    echo "### What this changed"
    echo
    if [ -n "$issue_num" ]; then
      echo "Closes #${issue_num}${issue_title:+ — $issue_title}"
      echo
    fi
    echo "$stats — ${gofiles} Go file(s), ${testfiles} of them tests."
    echo
    echo "$files" | sed 's/^/- `/; s/$/`/'
    echo
    echo "### What was verified"
    echo
    echo "- Every check green: $(awk -F'\t' '{print $1}' <<<"$runs" | paste -sd '|' - | sed 's/|/, /g')"
    if [ "$rabbit_skipped" = yes ]; then
      echo "- CodeRabbit did not review this PR (it is skipped for this repository), so no automated review is claimed"
    else
      echo "- CodeRabbit reviewed it and raised no actionable comments"
    fi
    [ -n "$scope_note" ] && echo "- Scope: $scope_note"
    echo "- No maintainer-owned path touched — nothing here can alter a published figure"
    echo "- No dependency change"
    echo
    echo "### What was not"
    echo
    echo "Whether the change is *correct*. Auto-merge verifies only what it can check mechanically — that the right files were touched, the tests pass, and the review happened. It forms no judgement about the substance, which is why it declines to merge anything touching pricing, the verdict thresholds, the integrity taxonomy, or the published chain."
  } | gh pr comment "$PR" --repo "$REPO" --body-file -
  exit 0
fi

# Not mergeable. Label for review and say exactly why, once.
printf 'not auto-merging:\n'
printf '  - %s\n' "${reasons[@]}"

gh pr edit "$PR" --repo "$REPO" --add-label "needs-maintainer-review" || true

marker="<!-- auto-merge-gate -->"
comment_body=$(
  echo "$marker"
  echo "### Auto-merge: held for review"
  echo
  echo "Thanks for the PR, @${author}. This is not a rejection: auto-merge only lands changes it can verify, and ${#reasons[@]} thing(s) stopped this one."
  echo
  for i in "${!reasons[@]}"; do
    echo "**$((i+1)). ${reasons[$i]^}**"
    echo
    echo "> **What to do:** ${fixes[$i]}"
    echo
  done
  if [ ${#waiting[@]} -gt 0 ]; then
    echo "_Also pending: $(printf '%s; ' "${waiting[@]}" | sed 's/; $//')._"
    echo
  fi
  echo "---"
  echo "This re-checks on every push, review and finished CI run, and this comment updates in place. Once everything above is resolved the PR merges automatically."
)

# One comment per PR, edited in place as the reasons change, so the
# contributor sees the current list, not a stale first verdict.
existing_id=$(gh api --paginate "repos/$REPO/issues/$PR/comments" \
  --jq '[.[] | select(.user.login == "github-actions[bot]" and (.body | contains("<!-- auto-merge-gate -->")))] | last | .id // empty')

if [ -z "$existing_id" ]; then
  gh pr comment "$PR" --repo "$REPO" --body "$comment_body"
else
  current=$(gh api "repos/$REPO/issues/comments/$existing_id" --jq '.body')
  if [ "$current" != "$comment_body" ]; then
    gh api -X PATCH "repos/$REPO/issues/comments/$existing_id" -f body="$comment_body" >/dev/null
  fi
fi
