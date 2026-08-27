package app

import (
	"errors"
	"fmt"
	"strings"
)

func printCompletion(arguments []string) error {
	if len(arguments) != 1 {
		return errors.New("usage: lectr completion zsh|bash")
	}
	script, err := completionScript(arguments[0])
	if err != nil {
		return err
	}
	fmt.Print(script)
	return nil
}

func completionScript(shell string) (string, error) {
	names := make([]string, len(publicCommands))
	zshEntries := make([]string, len(publicCommands))
	for index, command := range publicCommands {
		names[index] = command.Name
		zshEntries[index] = fmt.Sprintf("    '%s:%s'", command.Name, command.Description)
	}
	switch shell {
	case "zsh":
		return fmt.Sprintf(`#compdef lectr

_lectr() {
  local -a commands
  commands=(
    '--config:Use another config file'
%s
  )
  if (( CURRENT == 2 )); then
    _describe 'command' commands
    return
  fi
  case $words[2] in
    transcribe) _arguments '--config[use another config file]:path:_files' '--force[replace existing part transcripts]' '--dry-run[preview without changing files]' '1:course or date:' '2:date or memo:' ;;
    watch) _values 'action' install uninstall status ;;
    completion) _values 'shell' zsh bash ;;
  esac
}

compdef _lectr lectr
`, strings.Join(zshEntries, "\n")), nil
	case "bash":
		return fmt.Sprintf(`_lectr_completion() {
  local current command
  current="${COMP_WORDS[COMP_CWORD]}"
  command="${COMP_WORDS[1]}"
  if [[ $COMP_CWORD -eq 1 ]]; then
	COMPREPLY=( $(compgen -W '--config %s' -- "$current") )
  elif [[ $command == watch ]]; then
    COMPREPLY=( $(compgen -W 'install uninstall status' -- "$current") )
  elif [[ $command == completion ]]; then
    COMPREPLY=( $(compgen -W 'zsh bash' -- "$current") )
  elif [[ $command == transcribe ]]; then
    COMPREPLY=( $(compgen -W '--force --dry-run' -- "$current") )
  fi
}
complete -F _lectr_completion lectr
`, strings.Join(names, " ")), nil
	default:
		return "", fmt.Errorf("unsupported shell %q; choose zsh or bash", shell)
	}
}
