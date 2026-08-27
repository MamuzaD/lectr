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
%s
  )
  local curcontext="$curcontext" state line

  _arguments -C \
    '1: :->command' \
    '*::arg:->args'

  case $state in
    command)
      _describe -V 'command' commands
      ;;
    args)
      case $line[1] in
        transcribe) _arguments '--config[use another config file]:path:_files' '--force[replace existing part transcripts]' '--dry-run[preview without changing files]' '1:course or date:' '2:date or memo:' ;;
        watch) _arguments '--config[use another config file]:path:_files' '1:action:(install uninstall status)' ;;
        status) _arguments '--config[use another config file]:path:_files' ;;
        configure) _arguments '--config[use another config file]:path:_files' ;;
        completion) _values 'shell' zsh bash ;;
        help) _values -V 'command' %s ;;
      esac
      ;;
  esac
}

compdef _lectr lectr
`, strings.Join(zshEntries, "\n"), strings.Join(names, " ")), nil
	case "bash":
		return fmt.Sprintf(`_lectr_completion() {
  local current previous command
  current="${COMP_WORDS[COMP_CWORD]}"
  previous="${COMP_WORDS[COMP_CWORD-1]}"
  command="${COMP_WORDS[1]}"
  if [[ $COMP_CWORD -eq 1 ]]; then
    COMPREPLY=( $(compgen -W '%s' -- "$current") )
    return
  fi
  if [[ $previous == --config ]]; then
    COMPREPLY=( $(compgen -f -- "$current") )
    return
  fi
  case $command in
    watch) COMPREPLY=( $(compgen -W '--config install uninstall status' -- "$current") ) ;;
    status) COMPREPLY=( $(compgen -W '--config' -- "$current") ) ;;
    configure) COMPREPLY=( $(compgen -W '--config' -- "$current") ) ;;
    completion) COMPREPLY=( $(compgen -W 'zsh bash' -- "$current") ) ;;
    transcribe) COMPREPLY=( $(compgen -W '--config --force --dry-run' -- "$current") ) ;;
    help) COMPREPLY=( $(compgen -W '%s' -- "$current") ) ;;
  esac
}
complete -F _lectr_completion lectr
`, strings.Join(names, " "), strings.Join(names, " ")), nil
	default:
		return "", fmt.Errorf("unsupported shell %q; choose zsh or bash", shell)
	}
}
