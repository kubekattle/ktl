from datetime import datetime

features = [
    {
        "title": "Молниеносный просмотр логов",
        "lede": "Информеры Kubernetes позволяют ktl находить подходящие поды менее чем за секунду, поэтому инженеры получают поток логов сразу после ввода запроса.",
        "body": "Команда `ktl logs` сочетает поддержку регулярных выражений, селекторов и контекстных подсказок, что делает её более гибкой заменой стандартного `kubectl logs`. Набор флагов `--highlight`, `--events`, `--selector` и `--container` помогает формировать точные выборки для сценария «{use_case}».",
        "bullets": [
            "Поддержка `--since`, `--tail` и `--follow=false` упрощает анализ ретроспективных окон.",
            "Потоки можно преобразовать в JSON (`--json`, `--output extjson`) для последующей автоматизации.",
            "Шаблоны Go (`--template`, `--template-file`) позволяют собирать структурированные отчёты прямо в терминале."
        ]
    },
    {
        "title": "Объединение подовых и узловых логов",
        "lede": "ktl одновременно подхватывает kubelet и системные журналы, поэтому графики инцидента дополняются деталями с нод без отдельного ssh.",
        "body": "Флаги `--node-logs`, `--node-log /var/log/kubelet.log`, `--node-log-all` и `--node-log-only` раскрывают то, что происходило на инфраструктурном уровне во время сценария «{use_case}». Это экономит минуты на переключение контекстов и устраняет догадки об источнике деградации.",
        "bullets": [
            "При отсутствии совпадающих подов `--node-log-all` продолжает поток от нод, чтобы не терять симптоматиику.",
            "Фильтры по namespace и контейнерам остаются активными, даже когда добавлены системные файлы.",
            "Сочетание с `--events` позволяет видеть kubelet-события и системные логи синхронно."
        ]
    },
    {
        "title": "Совместные зеркала HTML и WebSocket",
        "lede": "Любой терминальный запуск `ktl logs` можно расшарить через браузер или WebSocket, не копируя команды коллегам.",
        "body": "Флаги `--ui :8080` и `--ws-listen :9080` создают зеркала, где вся подсветка и фильтры остаются управляемыми инициатором. Для сценария «{use_case}» это означает, что SRE-команда, бизнес и боты наблюдают за тем же стеком данных без дублирования нагрузки на кластер.",
        "bullets": [
            "Зрители не могут изменять фильтры, поэтому одна точка правды сохраняется автоматически.",
            "Сессии можно архивировать напрямую из браузера, сохраняя хронологию инцидента.",
            "WS-поток легко потребляется CI-ботами, которые реагируют на ключевые слова."
        ]
    },
    {
        "title": "Диагностический арсенал",
        "lede": "`ktl diag` покрывает квоты, CronJob, PodSecurity, storage и многое другое без написания скриптов.",
        "body": "Команды `ktl diag quotas`, `ktl diag nodes`, `ktl diag storage`, `ktl diag cronjobs`, `ktl diag network` и `ktl diag podsecurity` формируют готовые таблицы. В сценарии «{use_case}» они помогают быстро найти сдерживающий фактор и подготовить артефакты для постмортема.",
        "bullets": [
            "`ktl diag resources` объединяет requests/limits с live metrics.",
            "`ktl diag priorities` подсвечивает preemption и PriorityClasses.",
            "`ktl diag report --html` строит визуальные scorecards по тем же данным."
        ]
    },
    {
        "title": "Планирование деплоев",
        "lede": "`ktl deploy plan` визуализирует создаваемые, изменяемые и удаляемые манифесты до запуска установки.",
        "body": "Команда сравнивает chart + values с текущим кластером и отмечает объекты, влияющие на поды. В рамках «{use_case}» это избавляет от непредсказуемых рестартов и ускоряет согласования с командами сопровождения.",
        "bullets": [
            "Флаги `--chart`, `--release`, `--namespace`, `--values` повторяют семантику Helm.",
            "HTML вывод (`--html --output dist/plan.html`) превращает результат в артефакт для стейкхолдеров.",
            "Diff-формат подсвечивает изменения образов, env и PDB."
        ]
    },
    {
        "title": "Drift и снепшоты",
        "lede": "`ktl logs drift watch` и `ktl diag snapshot save` непрерывно отслеживают расхождения манифестов.",
        "body": "В сценарии «{use_case}» ktl фиксирует контрольные точки, а затем подсвечивает новые ReplicaSet, изменения хэшей и рестарты. Это основа для регрессной аналитики и аудита.",
        "bullets": [
            "`ktl logs capture` записывает логи и события в переносимые tarball/SQLite артефакты.",
            "`ktl logs capture replay` переигрывает инциденты офлайн.",
            "Report HTML может сравнивать два снепшота (`--compare-left`, `--compare-right`)."
        ]
    },
    {
        "title": "SQLite-пакеты приложений",
        "lede": "`ktl app package` собирает манифесты, образы, SBOM и аттестации в детерминированный `.k8s` файл.",
        "body": "Подход SQLite гарантирует, что всё содержание доступно через стандартные `SELECT`. Для «{use_case}» это означает единый артефакт для air-gap кластеров и цепочки поставки.",
        "bullets": [
            "Таблицы `metadata`, `manifests`, `images`, `attachments` покрывают весь спектр данных.",
            "`ktl app package verify` проверяет Ed25519 подпись и вложенные SLSA/SBOM.",
            "`ktl app unpack` извлекает снимки в файловую структуру."
        ]
    },
    {
        "title": "Контроль поставки",
        "lede": "Архив `.k8s` содержит provenance, лицензии и SBOM, поэтому аудиторы не требуют дополнительных выгрузок.",
        "body": "В сценарии «{use_case}» мы просто выполняем SQL-запросы к таблице `attachments`, чтобы доказать происхождение и состав релиза. Такая прозрачность редко встречается в проприетарных форматах.",
        "bullets": [
            "SLSA JSON фиксирует BuildKit, git SHA и параметры запуска.",
            "SBOM на базе SPDX 2.3 связывает каждый digest с лицензией.",
            "License summary позволяет классифицировать copyleft/пермиссивные пакеты в секунды."
        ]
    },
    {
        "title": "Вендоринг без дополнительных CLI",
        "lede": "`ktl app vendor` встраивает возможности vendir, поэтому синхронизация сторонних чартов происходит в одном бинарнике.",
        "body": "В сценарии «{use_case}» мы поддерживаем `vendir.yml`/`vendir.lock.yml`, подкачиваем Helm-чарты, git-каталоги и артефакты GitHub Releases без отдельного CLI и сложных установок.",
        "bullets": [
            "Поддерживаются `--directory`, мульти-конфигурации (`-f`), `--locked` режим.",
            "Уважает `VENDIR_CACHE_DIR`, `VENDIR_GITHUB_API_TOKEN` и другие env.",
            "Результаты складываются в `testdata/charts/` для детерминированных тестов."
        ]
    },
    {
        "title": "PostgreSQL резервные копии",
        "lede": "`ktl db backup` и `ktl db restore` работают прямо из подов, исключая ручные `kubectl exec`.",
        "body": "В сценарии «{use_case}» инженер снимает дампы всех БД (если флаг `--database` не задан), а затем восстанавливает их с помощью `--drop-db` и `--yes` без скачивания внешних утилит.",
        "bullets": [
            "Архивы автоматически сжимаются и именуются по времени.",
            "Восстановление показывает прогресс по каждой базе.",
            "Можно направлять выгрузки в каталог `backups/` или внешний bucket."
        ]
    },
    {
        "title": "Traffic forensics",
        "lede": "`ktl analyze traffic` добавляет эфемерный контейнер и снимает tcpdump в сетевом неймспейсе целевого пода.",
        "body": "Настройки `--between`, `--filter`, `--interface`, `--image` и пресеты `--bpf dns|handshake|postgres` создают точный профиль пакетов для «{use_case}» без модификации образов.",
        "bullets": [
            "Собственный образ можно указать через `--image`.",
            "Несколько `--target` позволяют захватывать разные неймспейсы.",
            "Результаты потока пригодны для SIEM и внутреннего мониторинга."
        ]
    },
    {
        "title": "Syscall профилирование",
        "lede": "`ktl analyze syscalls` подключает strace/bcc к шумным подам и агрегирует топ системных вызовов.",
        "body": "Флаги `--profile-duration`, `--match`, `--top` и `--target-pid` обеспечивают гибкость, а вывод можно переключить в JSON для пайплайнов. Это позволяет в сценарии «{use_case}» определить, где приложение тратит время.",
        "bullets": [
            "`--format json` облегчает автоматический парсинг.",
            "Профиль по умолчанию длится 30 секунд и покрывает весь под.",
            "`--image-pull-policy` и `--privileged` управляют безопасностью запуска."
        ]
    },
    {
        "title": "Scorecards и отчёты",
        "lede": "`ktl diag report --html` формирует визуальные панели здоровья с фильтрами, таймлайном и runbook-картами.",
        "body": "Именно здесь вступают в игру токены из DESIGN.md: `--surface`, `.panel`, `.score-card`, `.insight-stack`. Для «{use_case}» отчёт показывает квоты, PodSecurity, дельты SLO и выдаёт команды «Copy command».",
        "bullets": [
            "`?print` режим упрощает PDF.",
            "Фильтры по readiness/restars/violations ускоряют обзоры.",
            "История сохраняется в `~/.config/ktl/scorecard/history.db`."
        ]
    },
    {
        "title": "Конфигурация и палитры",
        "lede": "ktl поддерживает YAML-конфигурации (`~/.config/ktl/config.yaml`) и переменные окружения `KTL_*`.",
        "body": "В сценарии «{use_case}» команда может зафиксировать namespace, дефолтные подсветки, кастомные цвета подов (`pod-colors`) и форматы времени, чтобы стандартизировать вывод.",
        "bullets": [
            "`container-colors` поддерживает комбинации с подчёркиванием.",
            "`--timezone` и `--timestamp-format` улучшают кросс-региональные расследования.",
            "`--color=never` полезен для CI и логгеров."
        ]
    },
    {
        "title": "RBAC-aware режим",
        "lede": "ktl наследует разрешения kubeconfig и аккуратно репортит отказанные вызовы.",
        "body": "Это критично в сценарии «{use_case}», где разные команды работают в общих кластерах. CLI сообщает, какие API вернули `Forbidden`, помогая обновить ClusterRole без догадок.",
        "bullets": [
            "`--kubeconfig` и `--context` доступны на всех командах.",
            "`KTL_CONFIG` позволяет быстро переключать профили.",
            "Документация в `docs/config.all-options.yaml` даёт полный реестр флагов."
        ]
    },
    {
        "title": "Capture и реплей логов",
        "lede": "`ktl logs capture` записывает события, логи и состояния workloads в переносимые архивы.",
        "body": "Для «{use_case}» мы фиксируем пяти минутку (`--duration`) или ручной диапазон, затем `ktl logs capture replay` показывает материал офлайн, а `ktl logs capture diff` сравнивает метаданные разных попыток.",
        "bullets": [
            "Артефакты можно хранить в S3 и делиться с партнёрами.",
            "Diff подчёркивает изменения подов, флагов и namespaces.",
            "Повторное воспроизведение доступно без подключения к кластеру."
        ]
    },
    {
        "title": "Сбор метрик top",
        "lede": "`ktl diag top --all-namespaces` и `ktl top` дают быстрый обзор CPU/Memory без kubectl.",
        "body": "В сценарии «{use_case}» отчёт выводит лидеров по потреблению и выявляет runaway контейнеры. Опции сортировки и label selector поддерживают точечные фокусы.",
        "bullets": [
            "`--sort-cpu` и `--sort-memory` ускоряют разбор.",
            "`-A` даёт кластерный обзор, `-n` фиксирует пространство имён.",
            "Команда повторяет UX `kubectl top`, но доступна всегда там, где установлен ktl."
        ]
    },
    {
        "title": "BuildKit interactive build",
        "lede": "`ktl build` с `--interactive` позволяет провалиться внутрь Dockerfile шага прямо при сбое.",
        "body": "Для «{use_case}» мы используем `--interactive-shell \"bash -l\"`, чтобы воспроизвести окружение BuildKit и пофиксить образ до повторного запуска CI.",
        "bullets": [
            "Поддерживаются разные GOOS/GOARCH через Makefile таргеты.",
            "`testdata/build/interactive/Dockerfile` служит репро для обучения.",
            "Флаг требует TTY, поэтому подходит для локальных терминалов или `ssh -tt`."
        ]
    }
]

use_cases = [
    {
        "title": "Стабилизация checkout в prod-payments",
        "narrative": "Команда платёжного шлюза использует ktl, чтобы фиксировать деградации Checkout при пиковых распродажах. Логи подов `checkout-.*` сочетаются с `--highlight ERROR`, а HTML отчёт рассылается руководителям.",
        "impact": "В результате MTTR сократилось, а повторяемые рецепты помещены в runbook.",
        "benefits": [
            "Поддерживается трансляция в браузер через `--ui`.",
            "Node-логи рядом с приложением показывают проблемы kubelet.",
            "Scorecards отслеживают прогресс по PodSecurity и квотам."
        ]
    },
    {
        "title": "Подготовка canary-выпуска",
        "narrative": "Перед развёртыванием canary команда прогоняет `ktl deploy plan` с production values и собирает подписи заинтересованных сторон.",
        "impact": "Риски документируются заранее, а диффы по ReplicaSet доступны всем участникам.",
        "benefits": [
            "Сводка создаёт общий язык для SRE, разработчиков и менеджеров.",
            "HTML версия добавляется в PR как артефакт.",
            "Rollback сценарии фиксируются в runbook карточках."
        ]
    },
    {
        "title": "Аудит CronJob и расписаний",
        "narrative": "`ktl diag cronjobs` подсвечивает просроченные задания, ошибки выполнения и дрейф расписаний во множестве namespace.",
        "impact": "Команда избегает пропусков отчётности и обнаруживает проблемные CronJob до инцидента.",
        "benefits": [
            "Таблица выводит last schedule и счётчики успехов/ошибок.",
            "HTML отчёт облегчает разбор с менеджментом.",
            "Сравнение снепшотов показывает регресс после деплоя."
        ]
    },
    {
        "title": "Построение posture-отчёта",
        "narrative": "`ktl diag report --html` собирает квоты, PodSecurity и readiness метрики в один дашборд, оформленный по DESIGN.md.",
        "impact": "Руководители получают консолидированное состояние кластера в виде презентабельного PDF.",
        "benefits": [
            "Фильтры чипов выделяют namespace с проблемами.",
            "Insight stack показывает таймлайн тревог.",
            "Budget donuts дают мгновенную оценку headroom."
        ]
    },
    {
        "title": "Air-gap релиз через `.k8s`",
        "narrative": "Платформа собирает `ktl app package` и развозит один файл на изолированные сегменты сети.",
        "impact": "Поставка больше не зависит от OCI реестров и сложных зеркалиров.",
        "benefits": [
            "В архиве уже есть SBOM и лицензии.",
            "`ktl app package verify` проверяет подписи в приёмке.",
            "Вложенные manifests извлекаются обычным sqlite3."
        ]
    },
    {
        "title": "Сетевые расследования",
        "narrative": "`ktl analyze traffic` снимает tcpdump с prod-подов без перезапуска приложений, чтобы показать TLS-рукопожатия и задержки.",
        "impact": "Диагностическая команда подтверждает гипотезы о сетевых задержках быстрее, чем при ручном развёртывании DaemonSet.",
        "benefits": [
            "Профилируются только нужные контейнеры.",
            "BPF-пресеты экономят время на составление фильтров.",
            "Данные можно сразу передать в Wireshark."
        ]
    },
    {
        "title": "Профилирование syscall",
        "narrative": "В пиковые часы API тормозит, поэтому команда запускает `ktl analyze syscalls` и выявляет блокировки на `openat`.",
        "impact": "Чёткая статистика по системным вызовам избавляет от долгих предположений.",
        "benefits": [
            "JSON-вывод подключается к дашбордам.",
            "Можно указать конкретный PID приложения.",
            "Щадит производительность благодаря короткому окну сбора."
        ]
    },
    {
        "title": "Бэкапы PostgreSQL",
        "narrative": "ktl подключается к StatefulSet и создаёт тарболы с дампами каждой базы без установки psql на рабочей станции.",
        "impact": "Бэкапы стали частью того же инструмента, что используют SRE, и легко проверяются в post-mortem.",
        "benefits": [
            "`--drop-db` гарантирует чистое восстановление.",
            "`--output` выбирает каталог хранения.",
            "Логи операции добавляются в HTML отчёт."
        ]
    },
    {
        "title": "Drift watch для compliance",
        "narrative": "`ktl logs drift watch` фиксирует состояние namespace каждые N минут и сигнализирует, если Deployment ушёл от задекларированных шаблонов.",
        "impact": "Аудиторы получают доказательство стабильности и готовые снапшоты.",
        "benefits": [
            "История хранится локально и может быть приложена к тикетам.",
            "Сравнение capture-архивов показывает изменения образов.",
            "Интеграция с `diag report` визуализирует отклонения."
        ]
    },
    {
        "title": "CI/CD контроль",
        "narrative": "`ktl diag health -A --json --fail-on warn` запускается как gate в CI, блокируя релисы, если scorecard проседает.",
        "impact": "Автоматическое принятие решений снижает человеческий фактор.",
        "benefits": [
            "JSON протокол легко логируется.",
            "Порог `--threshold` настраивается.",
            "Можно параллельно запускать targeted tests."
        ]
    },
    {
        "title": "Сбор сигналов из kube-system",
        "narrative": "`ktl logs . --namespace kube-system --tail=0` позволяет наблюдать системные сервисы без риска забыть важный флаг kubectl.",
        "impact": "Инженеры экономят время и избегают копипасты длинных команд.",
        "benefits": [
            "`--exclude-pod kube-apiserver` отфильтровывает шум.",
            "`--field-selector spec.nodeName=...` сужает вывод.",
            "`--only-log-lines` упрощает парсинг."
        ]
    },
    {
        "title": "Разбор событий rollout",
        "narrative": "`ktl logs diff-deployments` показывает, как новые ReplicaSet отличаются от старых, и какие события происходят прямо сейчас.",
        "impact": "Заметный дрейф выявляется до того, как пользователи почувствуют эффект.",
        "benefits": [
            "Показывается связка событий и логов.",
            "Доступны те же фильтры по namespace и селекторам.",
            "HTML отчёт добавляет историю в виде таймлайна."
        ]
    },
    {
        "title": "Поддержка нескольких kubeconfig",
        "narrative": "`ktl logs payments-api --context minikube` и `--kubeconfig ~/.kube/nonprod` позволяют переключаться между площадками без редактирования файлов.",
        "impact": "Команды разработки и эксплуатации работают в унифицированном UX без риска перепутать контекст.",
        "benefits": [
            "Можно хранить конфиги в GitOps-хранилище и просто указывать путь.",
            "Переменные `KTL_ALL_NAMESPACES` и др. задаются на уровне CI.",
            "CLI явно пишет текущий context в заголовках отчёта."
        ]
    },
    {
        "title": "Security posture",
        "narrative": "`ktl diag podsecurity` выводит namespace с вероятными нарушениями и рекомендует метки.",
        "impact": "Security-команда тратит меньше времени на аудит вручную.",
        "benefits": [
            "Совмещается с отчётом scorecards.",
            "Указывает, какие контроллеры блокируются.",
            "Прикладывает команды для исправления через `Copy command`."
        ]
    },
    {
        "title": "Визуализация бюджета ресурсов",
        "narrative": "Budget donuts в отчётах показывают использование CPU/Memory относительно квот и позволяют кликать для фильтрации namespace.",
        "impact": "Product-менеджеры понимают, где нужен capacity plan, без доступа к Grafana.",
        "benefits": [
            "Консистентные цвета благодаря токенам DESIGN.md.",
            "Привязка к applyFilters() гарантирует единые фильтры.",
            "`@media print` скрывает интерактив при экспорте."
        ]
    },
    {
        "title": "Формирование знаний",
        "narrative": "Runbook cards в insight-колонке содержат проверенные инструкции, ссылки и `cta.copy` кнопки.",
        "impact": "При инциденте каждый инженер действует по одинаковому сценарию, а HTML-файл служит источником обучения.",
        "benefits": [
            "Кнопки копируют готовые `ktl diag ...` команды.",
            "Карточки можно помечать `warn/fail` для приоритизации.",
            "В PDF режиме карточки скрываются, отдавая место фактам."
        ]
    },
    {
        "title": "Интеграция с Build & Release",
        "narrative": "`make build`, `make install`, `make release` и таргеты cross-build позволяют выпускать ktl на разные архитектуры.",
        "impact": "DevOps имеют один Makefile для локальной разработки и CI.",
        "benefits": [
            "`ktl build` открывает интерактивные shell внутри проваленных RUN шагов.",
            "Готовые GOOS/GOARCH команды задокументированы в README.",
            "Сборки складываются в `bin/` и `dist/` согласно правилам."
        ]
    },
    {
        "title": "Сбор пользовательских историй",
        "narrative": "Примеры из `examples.md` помогают командам быстро находить нужные комбинации флагов и делиться ими во внутренних вики.",
        "impact": "Документированные сценарии становятся стандартизированными плейбуками.",
        "benefits": [
            "Более 30 примеров покрывают tail, фильтрацию, цвета и экспорт.",
            "Сценарии легко копируются в runbooks.",
            "Используется единая терминология kubectl."
        ]
    }
]

command_sets = [
    {
        "caption": "Цепочка команд для логирования и трансляции",
        "lines": [
            "ktl logs 'checkout-.*' \\",
            "  --namespace prod-payments \\",
            "  --highlight ERROR --highlight timeout \\",
            "  --ui :8080 --ws-listen :9080"
        ]
    },
    {
        "caption": "Выборка kube-system без шумных подов",
        "lines": [
            "ktl logs . --namespace kube-system --tail=0 \\",
            "  --exclude-pod kube-apiserver --only-log-lines"
        ]
    },
    {
        "caption": "Сравнение узловых логов",
        "lines": [
            "ktl logs 'checkout-.*' \\",
            "  --namespace prod-payments \\",
            "  --node-logs --node-log /var/log/kubelet.log \\",
            "  --node-log /var/log/syslog"
        ]
    },
    {
        "caption": "Диагностика квот и PodSecurity",
        "lines": [
            "ktl diag quotas -A",
            "ktl diag podsecurity --namespace prod-payments",
            "ktl diag report --html --output dist/posture.html"
        ]
    },
    {
        "caption": "Drift watch и capture",
        "lines": [
            "ktl logs drift watch --namespace prod-payments",
            "ktl logs capture --duration 5m --capture-output dist/drift.tar",
            "ktl logs capture diff dist/drift_a.tar dist/drift_b.tar"
        ]
    },
    {
        "caption": "Пакетирование и проверка",
        "lines": [
            "ktl app package \\",
            "  --chart ./deploy/checkout \\",
            "  --release checkout \\",
            "  --archive-file dist/checkout.k8s",
            "ktl app package verify --archive-file dist/checkout.k8s"
        ]
    },
    {
        "caption": "Планирование деплоя",
        "lines": [
            "ktl deploy plan \\",
            "  --chart ./deploy/checkout \\",
            "  --release checkout \\",
            "  --namespace prod-payments \\",
            "  --values values/prod.yaml",
            "ktl deploy plan --html --output dist/checkout-plan.html"
        ]
    },
    {
        "caption": "Vendor sync",
        "lines": [
            "ktl app vendor sync \\",
            "  --chdir deploy \\",
            "  --file vendir.yml \\",
            "  --lock-file vendir.lock.yml \\",
            "  --directory charts/grafana"
        ]
    },
    {
        "caption": "PostgreSQL backup & restore",
        "lines": [
            "ktl db backup postgresql-0 -n roedk-2 --output backups/",
            "ktl db restore postgresql-0 -n sandbox \\",
            "  --archive backups/db_backup_20251128_161103.tar.gz \\",
            "  --drop-db --yes"
        ]
    },
    {
        "caption": "Traffic forensics",
        "lines": [
            "ktl analyze traffic \\",
            "  --target roedk-2/roedk-nginx-pko-86bc555bb-nlcw4:nginx-pko \\",
            "  --filter 'port 443' --interface any"
        ]
    },
    {
        "caption": "Syscall профилирование",
        "lines": [
            "ktl analyze syscalls \\",
            "  --target payments/api-0 \\",
            "  --profile-duration 20s \\",
            "  --top 12 --match open,connect,execve"
        ]
    },
    {
        "caption": "Health в CI",
        "lines": [
            "ktl diag health -A --json --fail-on warn",
            "ktl diag report trend --days 7"
        ]
    },
    {
        "caption": "Мультикластерные логи",
        "lines": [
            "ktl logs dashboard --namespace kube-system --kubeconfig ~/.kube/nonprod",
            "ktl logs payments-api --context minikube"
        ]
    },
    {
        "caption": "Фокус на CronJob",
        "lines": [
            "ktl diag cronjobs --namespace data-jobs",
            "ktl logs 'rollout-.*' --namespace blue --events-only --tail=0"
        ]
    },
    {
        "caption": "Фильтрация подов по условиям",
        "lines": [
            "ktl logs . --namespace qa --condition ready=false --tail=0",
            "ktl logs . --all-namespaces --condition scheduled=false --tail=0"
        ]
    },
    {
        "caption": "Работа с шаблонами",
        "lines": [
            "ktl logs auth --namespace corp-sec --template '{{printf \"%s (%s/%s/%s/%s)\\n\" .Message .NodeName .Namespace .PodName .ContainerName}}'",
            "ktl logs backend --template-file ~/.config/ktl/templates/minimal.tpl"
        ]
    },
    {
        "caption": "Цветовые профили",
        "lines": [
            "podColors=\"38;2;255;97;136,38;2;169;220;118,38;2;255;216;102,38;2;120;220;232,38;2;171;157;242\"",
            "ktl logs deploy/checkout --pod-colors \"$podColors\"",
            "ktl logs cart --namespace shop --diff-container"
        ]
    },
    {
        "caption": "Работа со временем",
        "lines": [
            "ktl logs auth --namespace corp-sec --since=15m",
            "ktl logs auth --namespace corp-sec --timezone Asia/Tokyo",
            "ktl logs auth --namespace corp-sec --timestamp-format '2006-01-02 15:04:05'"
        ]
    },
    {
        "caption": "JSON и raw вывод",
        "lines": [
            "ktl logs ingress --namespace edge --json | jq .",
            "ktl logs billing --namespace finance --output extjson | jq .",
            "ktl logs worker --namespace batch --output raw --color=never"
        ]
    },
    {
        "caption": "Зеркальный репорт",
        "lines": [
            "ktl diag report --html --compare-left baseline.k8s --compare-right release.k8s",
            "ktl diag report --threshold 85 --notify json"
        ]
    }
]

metrics = [
    {"state": "pass", "title": "Готовность подов", "value": "99%", "delta": "+1.2% за неделю", "description": "Большинство подов отвечает условию Ready даже во время пиков."},
    {"state": "warn", "title": "Использование квот CPU", "value": "82%", "delta": "+9% к прошлому релизу", "description": "Нагрузка растёт быстрее плана; требуется контроль горизонтального масштабирования."},
    {"state": "fail", "title": "PodSecurity соответствие", "value": "71%", "delta": "-6% после миграции", "description": "Новые namespace не получили требуемые метки baseline."},
    {"state": "pass", "title": "Доступность CronJob", "value": "98.5%", "delta": "+0.7%", "description": "Расписания исполняются вовремя благодаря мониторингу ktl."},
    {"state": "warn", "title": "Использование PVC", "value": "88%", "delta": "+4%", "description": "Единичные namespace упираются в лимиты хранения."},
    {"state": "pass", "title": "Покрытие бэкапами", "value": "100%", "delta": "0%", "description": "Все критичные БД включены в ежедневные ktl db backup."},
    {"state": "warn", "title": "Сетевые перехваты", "value": "26 расследований", "delta": "+5 кейсов", "description": "Рост обращений к analyze traffic требует автоматизации фильтров."},
    {"state": "pass", "title": "Drift-инциденты", "value": "0 за 30 дней", "delta": "-2 случая", "description": "Включённый drift watch предотвращает неожиданные изменения."},
    {"state": "fail", "title": "Своевременность отчётов", "value": "78%", "delta": "-3%", "description": "Не все команды выгружают posture PDF в срок."},
    {"state": "pass", "title": "CI health gate", "value": "92% проходов", "delta": "+8%", "description": "Автоматический `diag health` блокирует релизы до устранения предупреждений."},
    {"state": "warn", "title": "Утилизация узлов", "value": "75%", "delta": "+12%", "description": "Скачок CPU на новых worker-нодаx требует `ktl diag nodes`."},
    {"state": "pass", "title": "Vendor sync SLA", "value": "99.4%", "delta": "+0.2%", "description": "`ktl app vendor` интегрирован в nightly pipeline без аварий."},
    {"state": "warn", "title": "Трафик TLS", "value": "43 ms P95", "delta": "+7 ms", "description": "`ktl analyze traffic` показывает рост латентности между checkout и core API."},
    {"state": "pass", "title": "Покрытие SBOM", "value": "100% архивов", "delta": "+100%", "description": "Каждый `.k8s` содержит SPDX и license summary."},
    {"state": "fail", "title": "Соблюдение runbook", "value": "69%", "delta": "-10%", "description": "Не все команды обновили CTA карточки под новые процедуры."}
]

insight_focus = [
    "Сократить MTTR благодаря автоматическим capture",
    "Расширить air-gap канал поставки",
    "Синхронизировать инструменты SRE и разработчиков",
    "Повысить прозрачность безопасности",
    "Ускорить аудит поставок",
    "Стандартизировать сбор логов",
    "Упростить расследования сети",
    "Подтвердить соблюдение квот",
    "Сделать отчёты читаемыми для бизнеса",
    "Автоматизировать вендоринг",
    "Перейти на SQL-first анализ",
    "Собрать хронологию для ретроспектив"
]

highlight_tags = [
    "Informer tail <1s",
    "Node+pod журналы",
    "HTML зеркала",
    "WS трансляция",
    "Diag toolkit",
    "Quota heatmaps",
    "PodSecurity",
    "CronJob guard",
    "Capture replay",
    "Drift watch",
    "SQLite архив",
    "SBOM встроен",
    "Ed25519 подпись",
    "tcpdump preset",
    "strace JSON",
    "Vendoring",
    "PG backup",
    "Air-gap delivery",
    "CI gate",
    "Context aware",
    "Color tokens",
    "Runbook CTA",
    "Budget donuts",
    "Timeline",
    "Filter chips",
    "applyFilters()",
    "Copy toast",
    "Print-safe",
    "Helm plan",
    "Diff deployments"
]

sections = []
num_pages = 100
now = datetime.now().strftime("%Y-%m-%d %H:%M")

for page in range(1, num_pages + 1):
    feature = features[(page - 1) % len(features)]
    use_case = use_cases[((page - 1) * 3) % len(use_cases)]
    command_set = command_sets[((page - 1) * 5) % len(command_sets)]
    metric = metrics[((page - 1) * 7) % len(metrics)]
    insight = insight_focus[((page - 1) * 11) % len(insight_focus)]
    tags = [highlight_tags[((page - 1) + offset * 13) % len(highlight_tags)] for offset in range(3)]

    feature_body = feature["body"].format(use_case=use_case["title"])
    bullets_html = "".join(f"<li>{item}</li>" for item in feature["bullets"])
    benefits_html = "".join(f"<li>{item}</li>" for item in use_case["benefits"])
    command_lines = "\n".join(command_set["lines"])

    section_html = f"""
    <section class=\"panel page\" id=\"page-{page:03d}\">
      <div class=\"page-header\">
        <span class=\"page-index\">Страница {page:03d}</span>
        <div>
          <p class=\"page-insight\">{insight}</p>
          <h2>{feature['title']}</h2>
        </div>
      </div>
      <div class=\"chip-row\">
        {''.join(f'<span class=\"chip\">{tag}</span>' for tag in tags)}
      </div>
      <p class=\"lede\">{feature['lede']}</p>
      <p>{feature_body}</p>
      <ul class=\"bullet-list\">{bullets_html}</ul>
      <div class=\"page-flex\">
        <div class=\"page-block\">
          <h3>{use_case['title']}</h3>
          <p>{use_case['narrative']}</p>
          <p>{use_case['impact']}</p>
          <ul>{benefits_html}</ul>
        </div>
        <div class=\"page-block\">
          <h4>{command_set['caption']}</h4>
          <pre><code>{command_lines}</code></pre>
        </div>
      </div>
      <article class=\"score-card {metric['state']}\">
        <div class=\"score-head\">
          <h5>{metric['title']}</h5>
          <span class=\"score-delta\">{metric['delta']}</span>
        </div>
        <p class=\"score-value\">{metric['value']}</p>
        <p class=\"score-copy\">{metric['description']}</p>
      </article>
    </section>
    """

    sections.append(section_html)

sections_html = "\n".join(sections)

style = """
:root {
  --surface: rgba(255,255,255,0.9);
  --surface-soft: rgba(255,255,255,0.82);
  --border: rgba(15,23,42,0.12);
  --text: #0f172a;
  --muted: rgba(15,23,42,0.65);
  --accent: #2563eb;
  --chip-bg: rgba(37,99,235,0.08);
  --chip-text: #1d4ed8;
  --warn: #fbbf24;
  --fail: #ef4444;
  --sparkline-color: #0ea5e9;
}
* {
  box-sizing: border-box;
}
body {
  margin: 0;
  font-family: "SF Pro Display", "SF Pro Text", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background: radial-gradient(circle at top, #ffffff 0%, #dce3f1 100%);
  color: var(--text);
  line-height: 1.6;
}
.chrome {
  max-width: 1200px;
  margin: 0 auto;
  padding: 48px 56px 72px;
}
header h1 {
  font-size: 2.8rem;
  font-weight: 600;
  letter-spacing: -0.04em;
  margin: 0;
}
header .subtitle {
  color: var(--muted);
  text-transform: uppercase;
  letter-spacing: 0.2em;
  margin-top: 0.4rem;
}
.panel {
  background: var(--surface);
  border: 1px solid rgba(255,255,255,0.2);
  border-radius: 28px;
  padding: 32px;
  margin-bottom: 32px;
  box-shadow: 0 40px 80px rgba(16,23,36,0.12);
  backdrop-filter: blur(18px);
}
.page-header {
  display: flex;
  justify-content: space-between;
  gap: 24px;
  flex-wrap: wrap;
  align-items: baseline;
}
.page-index {
  font-weight: 600;
  color: var(--muted);
  letter-spacing: 0.18em;
  text-transform: uppercase;
}
.page-insight {
  color: var(--muted);
  margin: 0;
}
.page h2 {
  margin: 0.2rem 0 0.6rem;
}
.chip-row {
  display: flex;
  gap: 0.6rem;
  flex-wrap: wrap;
  margin-bottom: 1rem;
}
.chip {
  background: var(--chip-bg);
  color: var(--chip-text);
  padding: 0.25rem 0.85rem;
  border-radius: 999px;
  font-size: 0.78rem;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.lede {
  font-size: 1.1rem;
  font-weight: 500;
}
.bullet-list {
  padding-left: 1.2rem;
}
.page-flex {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 24px;
  margin-top: 1.5rem;
}
.page-block {
  background: var(--surface-soft);
  border-radius: 24px;
  padding: 24px;
  border: 1px solid var(--border);
}
.page-block h3, .page-block h4 {
  margin-top: 0;
}
pre {
  background: #0f172a;
  color: #f8fafc;
  padding: 16px;
  border-radius: 16px;
  overflow-x: auto;
  font-size: 0.9rem;
}
.score-card {
  margin-top: 24px;
  border-radius: 24px;
  padding: 24px;
  background: linear-gradient(125deg, #bbf7d0, #86efac);
  color: #052e16;
}
.score-card.warn {
  background: linear-gradient(125deg, #fef3c7, #fde68a);
  color: #78350f;
}
.score-card.fail {
  background: linear-gradient(125deg, #fee2e2, #fecdd3);
  color: #7f1d1d;
}
.score-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  text-transform: uppercase;
  letter-spacing: 0.18em;
  font-size: 0.8rem;
}
.score-value {
  font-size: 2rem;
  margin: 0.4rem 0;
}
.score-copy {
  margin: 0;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 16px;
  margin: 32px 0;
}
.card {
  background: var(--surface-soft);
  border-radius: 24px;
  padding: 24px;
  border: 1px solid var(--border);
}
.card span {
  text-transform: uppercase;
  color: var(--muted);
  letter-spacing: 0.18em;
  font-size: 0.75rem;
}
.card strong {
  font-size: 1.8rem;
  display: block;
}
.card p {
  margin: 0.5rem 0 0;
}
@media print {
  body {
    background: #ffffff;
  }
  .panel {
    box-shadow: none;
    border: 1px solid #000;
    page-break-inside: avoid;
  }
  .page {
    page-break-after: always;
  }
  .chip-row, pre {
    border: 1px solid #000;
  }
}
"""

hero_cards = f"""
<div class=\"grid hero-cards\">
  <div class=\"card\">
    <span>Фокус</span>
    <strong>Глубокие обзоры ktl</strong>
    <p>Построено на README.md и examples.md</p>
  </div>
  <div class=\"card\">
    <span>Длина</span>
    <strong>100 страниц</strong>
    <p>Каждый блок печатается на отдельном листе</p>
  </div>
  <div class=\"card\">
    <span>Обновлено</span>
    <strong>{now}</strong>
    <p>Актуальная сводка для инженерных команд</p>
  </div>
</div>
"""

html = f"""<!DOCTYPE html>
<html lang=\"ru\">
<head>
  <meta charset=\"utf-8\" />
  <title>ktl — расширенный обзор возможностей</title>
  <style>{style}</style>
</head>
<body>
  <div class=\"chrome\">
    <header>
      <h1>ktl — операционный навигатор для Kubernetes</h1>
      <p class=\"subtitle\">Документ подготовлен на основе README.md, examples.md и правил DESIGN.md</p>
    </header>
    {hero_cards}
    {sections_html}
  </div>
</body>
</html>
"""

path = "dist/ktl_longform_ru.html"
with open(path, "w", encoding="utf-8") as f:
    f.write(html)

print(f"Сгенерирован файл {path} с {num_pages} страницами")
