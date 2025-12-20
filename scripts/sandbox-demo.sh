#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

ktl_bin="${KTL_BIN:-./bin/ktl}"
policy="${KTL_SANDBOX_CONFIG:-$repo_root/testdata/sandbox/linux-ci.cfg}"

baseline_ctx="$repo_root/testdata/sandbox-demo/baseline"
marker_ctx="$repo_root/testdata/sandbox-demo/host-marker"
marker_path="${MARKER_PATH:-/tmp/ktl-sandbox-demo-host-marker.txt}"

red() { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
yellow() { printf "\033[33m%s\033[0m\n" "$*"; }

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    red "Не найдено: $1"
    exit 2
  fi
}

contains() {
  local needle="$1"
  if command -v rg >/dev/null 2>&1; then
    rg -q --fixed-strings "$needle"
  else
    grep -Fq "$needle"
  fi
}

need go
need uname

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
if [[ "$os" != "linux" ]]; then
  red "Это демо рассчитано на Linux (сейчас: $os)"
  exit 2
fi

if [[ ! -x "$ktl_bin" ]]; then
  yellow "Не найден бинарник ktl по пути $ktl_bin — собираю: make build"
  make build >/dev/null
fi

if ! command -v nsjail >/dev/null 2>&1; then
  red "nsjail не найден. Для демо песочницы нужен установленный nsjail на хосте."
  exit 2
fi

if [[ ! -f "$policy" ]]; then
  red "Не найдена политика песочницы: $policy"
  exit 2
fi

pass_count=0
fail_count=0
skip_count=0

note_pass() { green "PASS: $*"; pass_count=$((pass_count+1)); }
note_fail() { red "FAIL: $*"; fail_count=$((fail_count+1)); }
note_skip() { yellow "SKIP: $*"; skip_count=$((skip_count+1)); }

cat >&2 <<EOF

================================================================================
ДЕМО: ktl build без песочницы vs ktl build в песочнице
================================================================================

Что показываем аудитории:
  1) В обычном режиме ktl на Linux переисполняется в песочнице.
  2) Если песочницу отключить (KTL_SANDBOX_DISABLE=1), сборка выполняется без этого слоя защиты.
  3) В permissive-настройках билдера недоверенный Dockerfile может пытаться читать "хостовые" файлы.
  4) В песочнице ktl такой доступ должен быть ограничен.

Площадка: Linux + nsjail.

Используем:
  ktl:    $ktl_bin
  policy: $policy

EOF

printf "\n[Тест 0] Базовая сборка в песочнице: должны увидеть баннер и вывод\n" >&2
baseline_out="$("$ktl_bin" build "$baseline_ctx" 2>&1 || true)"
if printf "%s" "$baseline_out" | contains "Running ktl build inside the sandbox" ; then
  note_pass "ktl сообщил, что работает внутри песочницы"
else
  note_fail "нет баннера про песочницу (проверьте nsjail/policy/окружение)"
fi
if printf "%s" "$baseline_out" | contains "baseline-ok" ; then
  note_pass "сборка реально исполнилась (наш маркер baseline-ok присутствует)"
else
  note_fail "сборка не исполнилась (в выводе нет baseline-ok)"
fi

printf "\n[Тест 1] Отключаем песочницу: баннера быть не должно, но сборка должна пройти\n" >&2
nosb_out="$(KTL_SANDBOX_DISABLE=1 "$ktl_bin" build "$baseline_ctx" 2>&1 || true)"
if printf "%s" "$nosb_out" | contains "Running ktl build inside the sandbox" ; then
  note_fail "баннер песочницы появился, хотя KTL_SANDBOX_DISABLE=1"
else
  note_pass "песочница отключена (баннера нет)"
fi
if printf "%s" "$nosb_out" | contains "baseline-ok" ; then
  note_pass "сборка прошла и без песочницы (контрольный прогон)"
else
  note_fail "сборка не прошла без песочницы"
fi

printf "\n[Тест 2] Демонстрация 'видимости хоста' (только если билдер permissive)\n" >&2
cat >&2 <<EOF
Сейчас мы создадим на хосте файл-маркер:
  $marker_path
и запустим сборку, которая пытается прочитать этот файл через bind-mount /tmp.

Ожидаемое поведение:
  - Без песочницы: возможно увидим HOST_MARKER:present и содержимое маркера.
  - В песочнице: должно быть HOST_MARKER:missing (маркер не виден).

Если билдер уже блокирует такие host-mount фичи — тест будет SKIP (это безопасно).
EOF

marker_value="ktl-demo-marker-$(date +%s)-$RANDOM"
printf "%s\n" "$marker_value" >"$marker_path"
chmod 0644 "$marker_path"

nosb_marker_out="$(KTL_SANDBOX_DISABLE=1 "$ktl_bin" build "$marker_ctx" 2>&1 || true)"
sandbox_marker_out="$(KTL_SANDBOX_CONFIG="$policy" "$ktl_bin" build "$marker_ctx" 2>&1 || true)"

nosb_present=false
sandbox_present=false

if printf "%s" "$nosb_marker_out" | contains "HOST_MARKER:present" && printf "%s" "$nosb_marker_out" | contains "$marker_value" ; then
  nosb_present=true
fi
if printf "%s" "$sandbox_marker_out" | contains "HOST_MARKER:present" && printf "%s" "$sandbox_marker_out" | contains "$marker_value" ; then
  sandbox_present=true
fi

if [[ "$nosb_present" != true ]]; then
  note_skip "билдер, похоже, блокирует host bind mounts — трюк не воспроизводится на этом хосте"
elif [[ "$sandbox_present" == true ]]; then
  note_fail "в песочнице маркер всё ещё виден (политика может пробрасывать /tmp или билдер обходит ограничения)"
else
  note_pass "маркер виден без песочницы и скрыт в песочнице (главная демонстрация)"
fi

printf "\n[Тест 3] Если песочница не стартует — это должно быть видно через --sandbox-logs\n" >&2
bad_policy="$repo_root/testdata/sandbox/does-not-exist.cfg"
logs_out="$(KTL_SANDBOX_CONFIG="$bad_policy" "$ktl_bin" build "$baseline_ctx" --sandbox-logs 2>&1 || true)"
if printf "%s" "$logs_out" | contains "sandbox config:" ; then
  note_pass "ошибка политики видна (нет 'тихого' выхода без сообщений)"
else
  note_fail "ошибка политики не показалась (ожидали sandbox config: ...)"
fi

printf "\nИтог: %d PASS, %d FAIL, %d SKIP\n" "$pass_count" "$fail_count" "$skip_count" >&2
if [[ "$fail_count" -gt 0 ]]; then
  exit 1
fi
