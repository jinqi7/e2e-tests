find_release_pipelines_from_pr() {
  local REPO=$1
  local PR_NUM=$2
 
  if [ -z "$REPO" ] || [ -z "$PR_NUM" ]; then
    echo "please provide repo and PR number, for example: find_tekton_tasks_in_pr tektoncd/catalog 2052" 
    return 1
  fi

  setup_workspace
  # need to removed
  #cd release-service-catalog || exit 1
  # need to restore
  clone_and_checkout_pr "$REPO" "$PR_NUM" || return 1
  
  # Declare global variables
  declare -a -g FOUND_PIPELINENAMES
  declare -a -g FOUND_INTERNAL_PIPELINENAMES
  declare -a TEKTON_INTERNAL_TASKS
  declare -a TEKTON_MANAGED_TASKS
  declare -a TEKTON_MANAGED_PIPELINES
  declare -a TEKTON_INTERNAL_PIPELINES
  
  # Find all tasks and pipelines and save them to arrays
  find_changed_tekton_tasks_pipelines

  # find managed pipelines for managed tasks and save them to $FOUND_PIPELINENAMES 
  if [ ${#TEKTON_MANAGED_TASKS[@]} -eq 0 ]; then
    echo "No managed Tekton Tasks found"
  else
    for task in "${TEKTON_MANAGED_TASKS[@]}"; do
      find_pipelines_using_task "$task" "managed"
    done
  fi
  
  if [ ${#TEKTON_INTERNAL_TASKS[@]} -eq 0 ]; then
    echo "No internal Tekton Tasks found"
  else
    for task in "${TEKTON_INTERNAL_TASKS[@]}"; do
      find_pipelines_using_task "$task" "internal"
    done
  fi
  
  # Deal with internal pipelines directly searched
  if [ ${#TEKTON_INTERNAL_PIPELINES[@]} -eq 0 ]; then
     echo "No internal Tekton Pipelines found"
  else
    while IFS= read -r file; do
      local pipeline_name
      pipeline_name=$(yq e '.metadata.name' "$file")
    
      found=false
      for f in "${FOUND_INTERNAL_PIPELINENAMES[@]}"; do
        if [[ "$f" == "$pipeline_name" ]]; then
          found=true
          break
        fi
      done

      if [[ "$found" == false ]]; then
        FOUND_INTERNAL_PIPELINENAMES+=("$pipeline_name")
      fi
    done <<< $TEKTON_INTERNAL_PIPELINES
  fi
  
  declare -a TEMP_MANAGED_PIPELINENAMES
  echo "FOUND_INTERNAL_PIPELINENAMES: ""${FOUND_INTERNAL_PIPELINENAMES[*]}"

  # Map internal pipelines to managed pipelines
  while IFS= read -r pipeline_name; do
    case "$pipeline_name" in
      "create-advisory"|"check-embargoed-cves"|"get-advisory-severity")
        TEMP_MANAGED_PIPELINENAMES+=("rh-advisories")
        ;;
      "update-fbc-catalog"|"publish-index-image-pipeline")
        TEMP_MANAGED_PIPELINENAMES+=("fbc-release")
        ;;
      "process-file-updates")
        TEMP_MANAGED_PIPELINENAMES+=("rh-advisories" "push-to-addons-registry" "rh-push-to-external-registry" "rh-push-to-registry-redhat-io")
        ;;
      "push-artifacts-to-cdn")
        TEMP_MANAGED_PIPELINENAMES+=("push-disk-images-to-cdn")
        ;;
      "simple-signing-pipeline")
        TEMP_MANAGED_PIPELINENAMES+=("fbc-release" "rh-advisories" "rh-push-to-external-registry" "rh-push-to-registry-redhat-io")
        ;;
      "push-disk-images")
        TEMP_MANAGED_PIPELINENAMES+=("push-disk-images-to-cdn" "push-disk-images-to-marketplaces")
        ;;
      *)
        continue
        ;;
    esac
  done <<< "${FOUND_INTERNAL_PIPELINENAMES[@]}"

  # Process managed pipelines that were directly searched
  if [ ${#TEKTON_MANAGED_PIPELINES[@]} -gt 0 ] ; then
    while IFS= read -r file; do
      local pipeline_name
      pipeline_name=$(yq e '.metadata.name' "$file")
      
      # Add pipeline if not already present
      if [[ ! " ${FOUND_PIPELINENAMES[*]} " =~ " ${pipeline_name} " ]]; then
        FOUND_PIPELINENAMES+=("$pipeline_name")
      fi
    done < <(printf '%s\n' "${TEKTON_MANAGED_PIPELINES[@]}")
  else
    echo "No managed Tekton Pipelines directly found"
  fi

  if [ ${#TEMP_MANAGED_PIPELINENAMES[@]} -gt 0 ]; then
    while IFS= read -r pipeline_name; do
      # Add pipeline if not already present
      if [[ ! " ${FOUND_PIPELINENAMES[*]} " =~ " ${pipeline_name} " ]]; then
        FOUND_PIPELINENAMES+=("$pipeline_name")
      fi
    done < <(printf '%s\n' "${TEMP_MANAGED_PIPELINENAMES[@]}")
  else
    echo "No managed Tekton Pipelines from internal pipelines found"
  fi

  if [ ${#FOUND_PIPELINENAMES[@]} -eq 0 ]; then
    export FOUND_PIPELINES=""
  else
    export FOUND_PIPELINES="${FOUND_PIPELINENAMES[*]}"
  fi
  echo "FOUND_PIPELINES:""$FOUND_PIPELINES"
 
  #cleanup_workspace
}

setup_workspace() {
  local WORK_DIR=".tmp_pr_check"
  #rm -rf "$WORK_DIR"
  #mkdir "$WORK_DIR"
  cd "$WORK_DIR" || exit 1
}

clone_and_checkout_pr() {
  local repo=$1
  local pr_num=$2

  #echo "git clone: https://github.com/$repo.git"
 # git clone "https://github.com/$repo.git" release-service-catalog
  cd release-service-catalog || exit 1

  echo "checkout PR #$pr_num"
  if ! gh pr checkout "$pr_num"; then
    echo "Failed to checkout PR"
    cd ../.. || exit 1
   # rm -rf ".tmp_pr_check"
    return 1
  fi
}

find_changed_tekton_tasks_pipelines() {
  
  echo "get different files from development branch..."
  local FILES
  FILES=$(git diff --name-only origin/development...HEAD)
  if [ -z "$FILES" ]; then
    echo "No files changed in PR"
    cd ../.. || exit 1
    rm -rf ".tmp_pr_check"
    return 0
  fi

  echo "filter Tekton Task YAML file and check if it includes kind: Task"
  echo

  while IFS= read -r file; do
    if [[ "$file" =~ ^tasks/internal/[^/]+/[^/]+\.ya?ml$ ]] && [ -f "$file" ]; then
      if grep -q -E 'kind: *Task' "$file"; then
        TEKTON_INTERNAL_TASKS+=("$file")
        echo "found internal Tekton Task: $file"
      fi
    elif [[ "$file" =~ ^tasks/managed/[^/]+/[^/]+\.ya?ml$ ]] && [ -f "$file" ]; then
      if grep -q -E 'kind: *Task' "$file"; then
        TEKTON_MANAGED_TASKS+=("$file")
        echo "found managed Tekton Task: $file"
      fi
    elif [[ "$file" =~ ^pipelines/managed/[^/]+/[^/]+\.ya?ml$ ]] && [ -f "$file" ]; then
      if grep -q -E 'kind: *Pipeline' "$file"; then
        TEKTON_MANAGED_PIPELINES+=("$file")
        echo "found managed Tekton pipeline: $file"
      fi
    elif [[ "$file" =~ ^pipelines/internal/[^/]+/[^/]+\.ya?ml$ ]] && [ -f "$file" ]; then
      if grep -q -E 'kind: *Pipeline' "$file"; then
        TEKTON_INTERNAL_PIPELINES+=("$file")
        echo "found internal Tekton pipeline: $file"
      fi
    fi
  done <<< "$FILES"

}

find_pipelines_using_task() {
  local task_file="$1"  # e.g., tasks/internal/task1/task1.yaml
  local pipeline_dir="$2"
  # 1. get Task name
  if [[ ! -f "$task_file" ]]; then
    echo "Error: Task file not found: $task_file" >&2
    return 1
  fi
  
  echo "Searching for pipelines using task: $task_file"

  for dir in "pipelines/$pipeline_dir" ; do
    if [ -d "$dir" ]; then
      while IFS= read -r pipeline_file; do
        if [ -f "$pipeline_file" ] && grep -q "value: *$task_file" "$pipeline_file"; then
          local pipeline_name
          pipeline_name=$(yq e '.metadata.name' "$pipeline_file")

          if [[ -z "$pipeline_name" || "$pipeline_name" == "null" ]]; then
            echo "Error: Could not extract pipeline name from $pipeline_file" >&2
            return 1
          fi

          found=false
          if [ "$pipeline_dir" = "managed" ]; then
            for f in "${FOUND_PIPELINENAMES[@]}"; do
              if [[ "$f" == "$pipeline_name" ]]; then
                found=true
                break
              fi
            done

            if [[ "$found" == false ]]; then
              FOUND_PIPELINENAMES+=("$pipeline_name")
            fi
          else
            for f in "${FOUND_INTERNAL_PIPELINENAMES[@]}"; do
              if [[ "$f" == "$pipeline_name" ]]; then
                found=true
                break
              fi
            done

            if [[ "$found" == false ]]; then
              FOUND_INTERNAL_PIPELINENAMES+=("$pipeline_name")
            fi
          fi
          echo "Found in: $pipeline_file and the pipeline_name is $pipeline_name"
        fi
      done < <(grep -rl "taskRef:" "$dir"/*/*.yaml 2>/dev/null)
    fi
  done
    
}

cleanup_workspace() {
  cd ../.. || exit 1
  rm -rf ".tmp_pr_check"
}

find_release_pipelines_from_pr konflux-ci/release-service-catalog 939
