#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

ktl_bin="${KTL_BIN:-./bin/ktl}"
policy="${KTL_SANDBOX_CONFIG:-$repo_root/testdata/sandbox/linux-ci.cfg}"
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

need uname
need docker

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
if [[ "$os" != "linux" ]]; then
  red "Это демо рассчитано на Linux (сейчас: $os)"
  exit 2
fi

if ! docker info >/dev/null 2>&1; then
  red "Docker daemon недоступен. Запустите docker и повторите."
  exit 2
fi

if [[ ! -x "$ktl_bin" ]]; then
  yellow "Не найден бинарник ktl по пути $ktl_bin — собираю: make build"
  make build >/dev/null
fi

printf "Using ktl: %s\n" "$ktl_bin" >&2
printf "Using sandbox policy: %s\n" "$policy" >&2

marker_value="ktl-demo-marker-$(date +%s)-$RANDOM"
printf "%s\n" "$marker_value" >"$marker_path"
chmod 0644 "$marker_path"

cat >&2 <<EOF

================================================================================
ДЕМО: "просто Docker" vs песочница ktl (объяснение для аудитории)
================================================================================

Что показываем:
  A) Docker с -v (volume mount) явно даёт контейнеру доступ к файлам хоста.
  B) Docker с проброшенным /var/run/docker.sock позволяет контейнеру обращаться к Docker-демону.
     На практике это часто означает "контейнер управляет хостом" (очень опасно на недоверенном коде).
  C) ktl build в песочнице ограничивает "неявную видимость" хоста во время сборки.

Используем:
  ktl:    $ktl_bin
  policy: $policy
  marker: $marker_path

EOF

printf "\n== Демо A: Docker с volume mount читает файл с хоста ==\n" >&2
docker run --rm -v /tmp:/host-tmp alpine:3.20 \
  sh -ceu "test -f /host-tmp/$(basename "$marker_path") && echo 'DOCKER:marker_present' && cat /host-tmp/$(basename "$marker_path")"
green "OK: контейнер Docker прочитал файл хоста через явный volume mount"

printf "\n== Демо B: docker.sock как 'пульт управления' Docker-демоном (безопасная проверка) ==\n" >&2
if [[ -S /var/run/docker.sock ]]; then
  docker run --rm -v /var/run/docker.sock:/var/run/docker.sock docker:28-cli version >/dev/null 2>&1 \
    && green "OK: контейнер смог обратиться к Docker-демону через docker.sock" \
    || yellow "SKIP: не удалось обратиться к демону из docker:cli (pull запрещён/политика демона)"
else
  yellow "SKIP: на хосте нет /var/run/docker.sock"
fi

printf "\n== Демо C: ktl sandbox ограничивает неявную видимость хоста при сборке ==\n" >&2
cat >&2 <<EOF
Сейчас мы запустим ktl build для Dockerfile, который *пытается* прочитать файл-маркер с хоста через bind-mount /tmp.

Ожидаемое поведение:
  - Без песочницы (KTL_SANDBOX_DISABLE=1): на permissive-билдере можем увидеть HOST_MARKER:present и значение маркера.
  - В песочнице: должно быть HOST_MARKER:missing.

Если билдер блокирует host bind mounts — это будет SKIP (безопасный дефолт на этой машине).
EOF

nosb_out="$(KTL_SANDBOX_DISABLE=1 "$ktl_bin" build "$marker_ctx" 2>&1 || true)"
sandbox_out="$(KTL_SANDBOX_CONFIG="$policy" "$ktl_bin" build "$marker_ctx" 2>&1 || true)"

nosb_present=false
sandbox_present=false

if printf "%s" "$nosb_out" | contains "HOST_MARKER:present" && printf "%s" "$nosb_out" | contains "$marker_value" ; then
  nosb_present=true
fi
if printf "%s" "$sandbox_out" | contains "HOST_MARKER:present" && printf "%s" "$sandbox_out" | contains "$marker_value" ; then
  sandbox_present=true
fi

if [[ "$nosb_present" != true ]]; then
  yellow "SKIP: билдер, похоже, блокирует host bind mounts — сравнение не воспроизводится на этом хосте"
  exit 0
fi

if [[ "$sandbox_present" == true ]]; then
  red "FAIL: песочница всё ещё показала маркер (политика могла пробросить /tmp или билдер обошёл ограничения)"
  exit 1
fi

green "PASS: маркер читается без песочницы ktl и скрыт в песочнице ktl"
