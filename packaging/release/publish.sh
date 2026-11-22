#!/usr/bin/env bash
set -euo pipefail

tag=${1:?specify the Git tag}
cd -- "${2:?specify the directory with installers and checksums}"
repo=${GITHUB_REPOSITORY:?GitHub repository is required}
repo_url="${GITHUB_SERVER_URL:?GitHub server URL is required}/$repo"
base_url="$repo_url/releases/download/$tag"

sha256sum --check --strict SHA256SUMS.txt
mapfile -t installers < <(cut -d ' ' -f 3- SHA256SUMS.txt)
assets=("${installers[@]}" SHA256SUMS.txt)
title=$tag
preview_commit=''
note_flags=(--generate-notes)
preview_flags=()
if [[ $tag == dev-* ]]; then
  preview_commit=${3:?specify the commit that built the preview}
  title="开发预览 ${tag#dev-}"
  note_flags=()
  preview_flags=(--prerelease --latest=false)
  ref="refs/tags/$tag"
  tagged_commit=$(gh api "repos/$repo/git/matching-refs/tags/$tag" |
    jq -r --arg ref "$ref" '.[] | select(.ref == $ref) | .object.sha')
  if [[ -z $tagged_commit ]]; then
    tagged_commit=$(gh api --method POST "repos/$repo/git/refs" -f "ref=$ref" -f "sha=$preview_commit" --jq '.object.sha')
  fi
  if [[ $tagged_commit != "$preview_commit" ]]; then
    echo "Preview tag $tag points to $tagged_commit instead of $preview_commit." >&2
    exit 1
  fi
fi

notes=$(mktemp)
trap 'rm -f -- "$notes"' EXIT

release_fields=isDraft,isPrerelease,databaseId
if ! release=$(gh release view "$tag" --repo "$repo" --json "$release_fields"); then
  platforms=('Windows x64' 'Linux amd64' 'macOS Apple Silicon' 'macOS Intel' 'Android arm64')
  {
    if [[ -n $preview_commit ]]; then
      printf '由 [main 构建记录](%s/actions/runs/%s) 生成，提交 `%s`。\n\n' "$repo_url" "$GITHUB_RUN_ID" "$preview_commit"
    fi
    printf '## 安装包\n\n| 平台 | 下载 |\n| --- | --- |\n'
    for index in "${!installers[@]}"; do
      printf '| %s | [%s](%s/%s) |\n' "${platforms[$index]}" "${installers[$index]}" "$base_url" "${installers[$index]}"
    done
    printf '\n[下载 SHA-256 校验文件](%s/SHA256SUMS.txt)\n' "$base_url"
    printf '\nWindows 安装包未签名。macOS 安装包未公证。\n'
    if [[ ${installers[4]} == *-preview.apk ]]; then
      printf '\nAndroid 提供预览版，使用独立的应用 ID。\n'
    fi
  } > "$notes"
  gh release create "$tag" --repo "$repo" --verify-tag --draft --title "$title" \
    --notes-file "$notes" "${note_flags[@]}" "${preview_flags[@]}"
  release=$(gh release view "$tag" --repo "$repo" --json "$release_fields")
fi

release_id=$(jq -er '.databaseId' <<< "$release")
remote_assets() {
  gh api --paginate --slurp "repos/$repo/releases/$release_id/assets?per_page=100" | jq 'add // []'
}
uploaded=$(remote_assets)
if [[ $(jq -r '.isDraft' <<< "$release") == false ]]; then
  if [[ -n $preview_commit && $(jq -r '.isPrerelease' <<< "$release") != true ]]; then
    echo "Published release $tag is not marked as a prerelease." >&2
    exit 1
  fi
  for asset in "${assets[@]}"; do
    jq -e --arg name "$asset" 'any(.[]; .name == $name and .state == "uploaded")' <<< "$uploaded" >/dev/null || {
      echo "Published release $tag is missing $asset. Review it on GitHub before proceeding." >&2
      exit 1
    }
  done
  echo "Release $tag is already published with all expected assets."
  exit 0
fi

has_expected_digest() {
  jq -e --arg name "$1" --arg digest "sha256:$2" \
    'any(.[]; .name == $name and .state == "uploaded" and .digest == $digest)' <<< "$uploaded" >/dev/null
}

pending=()
for asset in "${assets[@]}"; do
  hash=$(sha256sum -- "$asset")
  if ! has_expected_digest "$asset" "${hash%% *}"; then
    pending+=("$asset")
  fi
done
if ((${#pending[@]})); then
  gh release upload "$tag" --repo "$repo" --clobber "${pending[@]}"
fi

uploaded=$(remote_assets)
for asset in "${assets[@]}"; do
  hash=$(sha256sum -- "$asset")
  if ! has_expected_digest "$asset" "${hash%% *}"; then
    echo "Uploaded file did not match: $asset" >&2
    exit 1
  fi
done
gh release edit "$tag" --repo "$repo" --draft=false --verify-tag "${preview_flags[@]}"
if [[ -n ${GITHUB_STEP_SUMMARY:-} ]]; then
  printf '[下载 %s 安装包](%s/releases/tag/%s)\n' "$title" "$repo_url" "$tag" >> "$GITHUB_STEP_SUMMARY"
fi
