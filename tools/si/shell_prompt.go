package main

import (
	"fmt"
	"strings"
)

func buildShellRC(settings Settings) string {
	applySettingsDefaults(&settings)
	lines := []string{
		"[ -f /etc/bash.bashrc ] && . /etc/bash.bashrc",
		"[ -f ~/.bashrc ] && . ~/.bashrc",
	}

	if settings.Shell.Prompt.Enabled != nil && !*settings.Shell.Prompt.Enabled {
		return strings.Join(lines, "\n") + "\n"
	}

	prompt := settings.Shell.Prompt
	profileColor := promptColor(prompt.Colors.Profile, "cyan")
	cwdColor := promptColor(prompt.Colors.Cwd, "blue")
	gitColor := promptColor(prompt.Colors.Git, "magenta")
	symbolColor := promptColor(prompt.Colors.Symbol, "green")
	resetColor := promptColor(prompt.Colors.Reset, "reset")
	prefixTemplate := prompt.PrefixTemplate
	if prefixTemplate == "" {
		prefixTemplate = "[{profile}] "
	}
	format := prompt.Format
	if format == "" {
		format = "{prefix}{cwd}{git} {symbol} "
	}
	symbol := prompt.Symbol
	if symbol == "" {
		symbol = "$"
	}
	gitEnabled := true
	if prompt.GitEnabled != nil {
		gitEnabled = *prompt.GitEnabled
	}

	lines = append(lines,
		fmt.Sprintf("__si_prompt_color_profile=%s", shellSingleQuote(profileColor)),
		fmt.Sprintf("__si_prompt_color_cwd=%s", shellSingleQuote(cwdColor)),
		fmt.Sprintf("__si_prompt_color_git=%s", shellSingleQuote(gitColor)),
		fmt.Sprintf("__si_prompt_color_symbol=%s", shellSingleQuote(symbolColor)),
		fmt.Sprintf("__si_prompt_color_reset=%s", shellSingleQuote(resetColor)),
		fmt.Sprintf("__si_prompt_prefix_template=%s", shellSingleQuote(prefixTemplate)),
		fmt.Sprintf("__si_prompt_format=%s", shellSingleQuote(format)),
		fmt.Sprintf("__si_prompt_symbol=%s", shellSingleQuote(symbol)),
		fmt.Sprintf("__si_prompt_git_enabled=%d", boolToInt(gitEnabled)),
		`__si_prompt() {
  local profile="${SI_CODEX_PROFILE_NAME:-}"
  local prefix=""
  if [ -n "$profile" ]; then
    prefix="${__si_prompt_prefix_template//\{profile\}/$profile}"
    prefix="${__si_prompt_color_profile}${prefix}${__si_prompt_color_reset}"
  fi
  local cwd="${__si_prompt_color_cwd}\w${__si_prompt_color_reset}"
  local git=""
  if [ "${__si_prompt_git_enabled:-1}" = "1" ]; then
    if type __git_ps1 >/dev/null 2>&1; then
      git="$(__git_ps1 " ${__si_prompt_color_git}(%s)${__si_prompt_color_reset}")"
    elif command -v git >/dev/null 2>&1; then
      local branch
      branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null)"
      if [ -n "$branch" ]; then
        git=" ${__si_prompt_color_git}(${branch})${__si_prompt_color_reset}"
      fi
    fi
  fi
  local symbol="${__si_prompt_color_symbol}${__si_prompt_symbol}${__si_prompt_color_reset}"
  local ps1="$__si_prompt_format"
  ps1="${ps1//\{prefix\}/$prefix}"
  ps1="${ps1//\{cwd\}/$cwd}"
  ps1="${ps1//\{git\}/$git}"
  ps1="${ps1//\{symbol\}/$symbol}"
  PS1="$ps1"
}
PROMPT_COMMAND="__si_prompt${PROMPT_COMMAND:+; $PROMPT_COMMAND}"`,
	)

	return strings.Join(lines, "\n") + "\n"
}

func promptColor(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "ansi:") {
		code := strings.TrimSpace(value[5:])
		if code == "" {
			return ""
		}
		if strings.Contains(code, "\x1b") || strings.Contains(code, "\033") || strings.Contains(code, "\\033") {
			return code
		}
		return ansiWrap(code)
	}
	if strings.HasPrefix(lower, "raw:") {
		return value[4:]
	}
	if lower == "reset" {
		return ansiWrap("0")
	}
	if code, ok := ansiColorMap()[lower]; ok {
		return ansiWrap(code)
	}
	return ansiWrap("0")
}

func ansiWrap(code string) string {
	return `\[\033[` + code + "m\\]"
}

func ansiColorMap() map[string]string {
	return map[string]string{
		"black":          "0;30",
		"red":            "0;31",
		"green":          "0;32",
		"yellow":         "0;33",
		"blue":           "0;34",
		"magenta":        "0;35",
		"cyan":           "0;36",
		"white":          "0;37",
		"bright-black":   "1;30",
		"bright-red":     "1;31",
		"bright-green":   "1;32",
		"bright-yellow":  "1;33",
		"bright-blue":    "1;34",
		"bright-magenta": "1;35",
		"bright-cyan":    "1;36",
		"bright-white":   "1;37",
	}
}

func shellSingleQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func boolToInt(val bool) int {
	if val {
		return 1
	}
	return 0
}
