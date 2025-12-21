#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

ktl_bin="${KTL_BIN:-./bin/ktl}"
policy="${KTL_SANDBOX_CONFIG:-$repo_root/testdata/sandbox/linux-ci.cfg}"

baseline_ctx="$repo_root/testdata/sandbox-demo/baseline"
probe_host_root_file="${PROBE_HOST_ROOT_FILE:-$repo_root/.ktl-sandbox-demo-host-only.txt}"
probe_ctx_file_rel="probe-visible-in-context.txt"
probe_ctx_file="$baseline_ctx/$probe_ctx_file_rel"

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
    rg -q --fixed-strings -- "$needle" -
  else
    grep -Fq "$needle"
  fi
}

require_probe_line() {
  local output="$1"
  local path="$2"
  if ! printf "%s" "$output" | contains "[probe] stat \"$path\":" ; then
    note_fail "нет строки probe в выводе (вместо этого была ошибка запуска/песочницы?)"
    return 1
  fi
  return 0
}

need uname

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
if [[ "$os" != "linux" ]]; then
  red "Это демо рассчитано на Linux (сейчас: $os)"
  exit 2
fi

if [[ ! -x "$ktl_bin" ]]; then
  need go
  need make
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

note_pass() { green "PASS: $*"; pass_count=$((pass_count+1)); }
note_fail() { red "FAIL: $*"; fail_count=$((fail_count+1)); }

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

if command -v git >/dev/null 2>&1 && [[ -d "$repo_root/.git" ]]; then
  printf "Версия репозитория (git): %s\n\n" "$(git rev-parse --short HEAD 2>/dev/null || true)" >&2
fi

printf "\n[Тест 0] Базовая сборка в песочнице: должны увидеть баннер и вывод\n" >&2
baseline_out="$("$ktl_bin" build "$baseline_ctx" --sandbox-config "$policy" 2>&1 || true)"
if printf "%s" "$baseline_out" | contains "Running ktl build inside the sandbox" ; then
  note_pass "ktl сообщил, что работает внутри песочницы"
else
  note_fail "нет баннера про песочницу (проверьте nsjail/policy/окружение)"
fi
if printf "%s" "$baseline_out" | contains "Built " || printf "%s" "$baseline_out" | contains "Build finished" ; then
  note_pass "сборка реально исполнилась (видим финальный результат)"
else
  note_fail "сборка не исполнилась (в выводе нет признаков завершения)"
fi

printf "\n[Тест 1] Отключаем песочницу: баннера быть не должно, но сборка должна пройти\n" >&2
nosb_out="$(KTL_SANDBOX_DISABLE=1 "$ktl_bin" build "$baseline_ctx" --sandbox-config "$policy" 2>&1 || true)"
if printf "%s" "$nosb_out" | contains "Running ktl build inside the sandbox" ; then
  note_fail "баннер песочницы появился, хотя KTL_SANDBOX_DISABLE=1"
else
  note_pass "песочница отключена (баннера нет)"
fi
if printf "%s" "$nosb_out" | contains "Built " || printf "%s" "$nosb_out" | contains "Build finished" ; then
  note_pass "сборка прошла и без песочницы (контрольный прогон)"
else
  note_fail "сборка не прошла без песочницы"
fi

printf "\n[Тест 2] Детерминированный probe: песочница прячет 'хостовые' файлы вне контекста\n" >&2
cat >&2 <<EOF
Это проверка НЕ зависит от BuildKit и не уходит в SKIP.

Мы создаём файл на хосте (в корне репозитория, ВНЕ build context):
  $probe_host_root_file

Дальше просим ktl сделать пробу ДО сборки:
  --sandbox-probe-path "$probe_host_root_file"

Ожидаемое поведение:
  - Без песочницы: [probe] stat ...: OK
  - В песочнице:   [probe] stat ...: ... no such file or directory

Почему это важно: песочница меняет "точку зрения" процесса ktl — он не видит произвольные файлы хоста.
EOF

printf "demo-probe-host-root-%s\n" "$(date +%s)" >"$probe_host_root_file"
chmod 0644 "$probe_host_root_file"

nosb_probe_out="$(KTL_SANDBOX_DISABLE=1 "$ktl_bin" build "$baseline_ctx" --sandbox-probe-path "$probe_host_root_file" 2>&1 || true)"
sandbox_probe_out="$("$ktl_bin" build "$baseline_ctx" --sandbox-config "$policy" --sandbox-probe-path "$probe_host_root_file" 2>&1 || true)"

require_probe_line "$nosb_probe_out" "$probe_host_root_file" || true
require_probe_line "$sandbox_probe_out" "$probe_host_root_file" || true

if printf "%s" "$nosb_probe_out" | contains "[probe] stat \"$probe_host_root_file\": OK" ; then
  note_pass "без песочницы ktl видит host-only файл (probe OK)"
else
  note_fail "ожидали probe OK без песочницы, но не увидели"
fi

if printf "%s" "$sandbox_probe_out" | contains "[probe] stat \"$probe_host_root_file\": OK" ; then
  note_fail "в песочнице probe неожиданно OK (файл хоста виден) — политика может пробрасывать лишние пути"
else
  note_pass "в песочнице ktl НЕ видит host-only файл (probe не OK) — ожидаемо"
fi

printf "\n[Тест 3] Детерминированный probe: контекст сборки виден и с песочницей, и без\n" >&2
cat >&2 <<EOF
Сейчас мы создадим файл прямо в build context:
  $probe_ctx_file

И проверим, что он доступен:
  - без песочницы (ожидаемо),
  - в песочнице (это важно: песочница не ломает обычный билд).

Важно: внутри песочницы ktl маппит build context в /workspace, поэтому для sandbox-run мы
проверяем probe по относительному пути (относительно /workspace).
EOF

printf "demo-probe-context-%s\n" "$(date +%s)" >"$probe_ctx_file"
chmod 0644 "$probe_ctx_file"

nosb_ctx_probe_out="$(KTL_SANDBOX_DISABLE=1 "$ktl_bin" build "$baseline_ctx" --sandbox-probe-path "$probe_ctx_file" 2>&1 || true)"
sandbox_ctx_probe_out="$("$ktl_bin" build "$baseline_ctx" --sandbox-config "$policy" --sandbox-probe-path "$probe_ctx_file_rel" 2>&1 || true)"

require_probe_line "$nosb_ctx_probe_out" "$probe_ctx_file" || true
require_probe_line "$sandbox_ctx_probe_out" "$probe_ctx_file_rel" || true

if printf "%s" "$nosb_ctx_probe_out" | contains "[probe] stat \"$probe_ctx_file\": OK" ; then
  note_pass "без песочницы файл в контексте виден (probe OK)"
else
  note_fail "ожидали probe OK для файла в контексте без песочницы"
fi

if printf "%s" "$sandbox_ctx_probe_out" | contains "[probe] stat \"$probe_ctx_file_rel\": OK" ; then
  note_pass "в песочнице файл в контексте виден (probe OK) — билд не сломан"
else
  note_fail "ожидали probe OK для файла в контексте в песочнице"
fi

printf "\n[Тест 4] Демонстрация allowlist: явный --sandbox-bind делает файл видимым\n" >&2
cat >&2 <<EOF
Важно донести аудитории: песочница — это allowlist.
Если нам действительно нужно дать доступ, мы делаем это ЯВНО.

Сейчас мы пробросим один конкретный файл:
  --sandbox-bind "$probe_host_root_file:/workspace/host-only.txt"

Ожидаемое поведение:
  - В песочнице без bind: probe НЕ OK (см. Тест 2)
  - В песочнице с bind:  probe OK
EOF

sandbox_bind_probe_out="$("$ktl_bin" build "$baseline_ctx" --sandbox-config "$policy" --sandbox-bind "$probe_host_root_file:/workspace/host-only.txt" --sandbox-probe-path "host-only.txt" 2>&1 || true)"
require_probe_line "$sandbox_bind_probe_out" "host-only.txt" || true
if printf "%s" "$sandbox_bind_probe_out" | contains "[probe] stat \"host-only.txt\": OK" ; then
  note_pass "явный --sandbox-bind сработал: host-only файл стал видимым"
else
  note_fail "ожидали probe OK с явным --sandbox-bind, но не увидели"
fi

printf "\n[Тест 5] Если песочница не стартует — это должно быть видно через --sandbox-logs\n" >&2
bad_policy="$repo_root/testdata/sandbox/does-not-exist.cfg"
logs_out="$("$ktl_bin" build "$baseline_ctx" --sandbox-config "$bad_policy" --sandbox-logs 2>&1 || true)"
if printf "%s" "$logs_out" | contains "sandbox config:" ; then
  note_pass "ошибка политики видна (нет 'тихого' выхода без сообщений)"
else
  note_fail "ошибка политики не показалась (ожидали sandbox config: ...)"
fi

printf "\nИтог: %d PASS, %d FAIL\n" "$pass_count" "$fail_count" >&2
if [[ "$fail_count" -gt 0 ]]; then
  exit 1
fi
