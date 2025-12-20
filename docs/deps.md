# Dependency Map (Generated)

This file is generated. Do not edit by hand.

Regenerate with:

```bash
make deps
```

## `github.com/example/ktl/cmd/ktl`

**Internal deps**

- `github.com/example/ktl/internal/api/convert`
- `github.com/example/ktl/internal/capture`
- `github.com/example/ktl/internal/caststream`
- `github.com/example/ktl/internal/castutil`
- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/csvutil`
- `github.com/example/ktl/internal/deploy`
- `github.com/example/ktl/internal/dockerconfig`
- `github.com/example/ktl/internal/drift`
- `github.com/example/ktl/internal/featureflags`
- `github.com/example/ktl/internal/grpcutil`
- `github.com/example/ktl/internal/kube`
- `github.com/example/ktl/internal/logging`
- `github.com/example/ktl/internal/mirrorbus`
- `github.com/example/ktl/internal/sqlitewriter`
- `github.com/example/ktl/internal/tailer`
- `github.com/example/ktl/internal/ui`
- `github.com/example/ktl/internal/workflows/buildsvc`
- `github.com/example/ktl/pkg/api/v1`
- `github.com/example/ktl/pkg/buildkit`
- `github.com/example/ktl/pkg/compose`
- `github.com/example/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/asaskevich/govalidator`
- `github.com/blang/semver/v4`
- `github.com/chai2010/gettext-go`
- `github.com/chai2010/gettext-go/mo`
- `github.com/chai2010/gettext-go/plural`
- `github.com/chai2010/gettext-go/po`
- `github.com/compose-spec/compose-go/v2/consts`
- `github.com/compose-spec/compose-go/v2/dotenv`
- `github.com/compose-spec/compose-go/v2/errdefs`
- `github.com/compose-spec/compose-go/v2/format`
- `github.com/compose-spec/compose-go/v2/graph`
- `github.com/compose-spec/compose-go/v2/interpolation`
- `github.com/compose-spec/compose-go/v2/loader`
- `github.com/compose-spec/compose-go/v2/override`
- `github.com/compose-spec/compose-go/v2/paths`
- `github.com/compose-spec/compose-go/v2/schema`
- `github.com/compose-spec/compose-go/v2/template`
- `github.com/compose-spec/compose-go/v2/transform`
- `github.com/compose-spec/compose-go/v2/tree`
- `github.com/compose-spec/compose-go/v2/types`
- `github.com/compose-spec/compose-go/v2/utils`
- `github.com/compose-spec/compose-go/v2/validation`
- `github.com/containerd/console`
- `github.com/containerd/containerd/api/services/content/v1`
- `github.com/containerd/containerd/archive/compression`
- `github.com/containerd/containerd/content`
- `github.com/containerd/containerd/errdefs`
- `github.com/containerd/containerd/filters`
- `github.com/containerd/containerd/images`
- `github.com/containerd/containerd/labels`
- `github.com/containerd/containerd/pkg/randutil`
- `github.com/containerd/containerd/remotes`
- `github.com/containerd/containerd/v2/core/content`
- `github.com/containerd/containerd/v2/core/content/proxy`
- `github.com/containerd/containerd/v2/core/images`
- `github.com/containerd/containerd/v2/core/leases`
- `github.com/containerd/containerd/v2/core/remotes`
- `github.com/containerd/containerd/v2/core/remotes/docker`
- `github.com/containerd/containerd/v2/core/remotes/docker/auth`
- `github.com/containerd/containerd/v2/core/remotes/errors`
- `github.com/containerd/containerd/v2/core/transfer`
- `github.com/containerd/containerd/v2/defaults`
- `github.com/containerd/containerd/v2/internal/fsverity`
- `github.com/containerd/containerd/v2/internal/lazyregexp`
- `github.com/containerd/containerd/v2/internal/randutil`
- `github.com/containerd/containerd/v2/pkg/archive/compression`
- `github.com/containerd/containerd/v2/pkg/filters`
- `github.com/containerd/containerd/v2/pkg/identifiers`
- `github.com/containerd/containerd/v2/pkg/labels`
- `github.com/containerd/containerd/v2/pkg/namespaces`
- `github.com/containerd/containerd/v2/pkg/protobuf`
- `github.com/containerd/containerd/v2/pkg/protobuf/types`
- `github.com/containerd/containerd/v2/pkg/reference`
- `github.com/containerd/containerd/v2/pkg/tracing`
- `github.com/containerd/containerd/v2/plugins/content/local`
- `github.com/containerd/containerd/v2/plugins/services/content/contentserver`
- `github.com/containerd/containerd/v2/version`
- `github.com/containerd/continuity/sysx`
- `github.com/containerd/errdefs`
- `github.com/containerd/errdefs/pkg/errgrpc`
- `github.com/containerd/errdefs/pkg/internal/cause`
- `github.com/containerd/errdefs/pkg/internal/types`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/containerd/stargz-snapshotter/estargz`
- `github.com/containerd/stargz-snapshotter/estargz/errorutil`
- `github.com/containerd/ttrpc`
- `github.com/containerd/typeurl/v2`
- `github.com/cyphar/filepath-securejoin`
- `github.com/cyphar/filepath-securejoin/internal/consts`
- `github.com/davecgh/go-spew/spew`
- `github.com/distribution/reference`
- ... (1134 more)

**Stdlib deps**

- 234 packages

## `github.com/example/ktl/cmd/ktl-agent`

**Internal deps**

- `github.com/example/ktl/internal/agent`
- `github.com/example/ktl/internal/api/convert`
- `github.com/example/ktl/internal/capture`
- `github.com/example/ktl/internal/caststream`
- `github.com/example/ktl/internal/castutil`
- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/csvutil`
- `github.com/example/ktl/internal/deploy`
- `github.com/example/ktl/internal/dockerconfig`
- `github.com/example/ktl/internal/drift`
- `github.com/example/ktl/internal/kube`
- `github.com/example/ktl/internal/logging`
- `github.com/example/ktl/internal/sqlitewriter`
- `github.com/example/ktl/internal/tailer`
- `github.com/example/ktl/internal/workflows/buildsvc`
- `github.com/example/ktl/pkg/api/v1`
- `github.com/example/ktl/pkg/buildkit`
- `github.com/example/ktl/pkg/compose`
- `github.com/example/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/asaskevich/govalidator`
- `github.com/blang/semver/v4`
- `github.com/chai2010/gettext-go`
- `github.com/chai2010/gettext-go/mo`
- `github.com/chai2010/gettext-go/plural`
- `github.com/chai2010/gettext-go/po`
- `github.com/compose-spec/compose-go/v2/consts`
- `github.com/compose-spec/compose-go/v2/dotenv`
- `github.com/compose-spec/compose-go/v2/errdefs`
- `github.com/compose-spec/compose-go/v2/format`
- `github.com/compose-spec/compose-go/v2/graph`
- `github.com/compose-spec/compose-go/v2/interpolation`
- `github.com/compose-spec/compose-go/v2/loader`
- `github.com/compose-spec/compose-go/v2/override`
- `github.com/compose-spec/compose-go/v2/paths`
- `github.com/compose-spec/compose-go/v2/schema`
- `github.com/compose-spec/compose-go/v2/template`
- `github.com/compose-spec/compose-go/v2/transform`
- `github.com/compose-spec/compose-go/v2/tree`
- `github.com/compose-spec/compose-go/v2/types`
- `github.com/compose-spec/compose-go/v2/utils`
- `github.com/compose-spec/compose-go/v2/validation`
- `github.com/containerd/console`
- `github.com/containerd/containerd/api/services/content/v1`
- `github.com/containerd/containerd/archive/compression`
- `github.com/containerd/containerd/content`
- `github.com/containerd/containerd/errdefs`
- `github.com/containerd/containerd/filters`
- `github.com/containerd/containerd/images`
- `github.com/containerd/containerd/labels`
- `github.com/containerd/containerd/pkg/randutil`
- `github.com/containerd/containerd/remotes`
- `github.com/containerd/containerd/v2/core/content`
- `github.com/containerd/containerd/v2/core/content/proxy`
- `github.com/containerd/containerd/v2/core/images`
- `github.com/containerd/containerd/v2/core/leases`
- `github.com/containerd/containerd/v2/core/remotes`
- `github.com/containerd/containerd/v2/core/remotes/docker`
- `github.com/containerd/containerd/v2/core/remotes/docker/auth`
- `github.com/containerd/containerd/v2/core/remotes/errors`
- `github.com/containerd/containerd/v2/core/transfer`
- `github.com/containerd/containerd/v2/defaults`
- `github.com/containerd/containerd/v2/internal/fsverity`
- `github.com/containerd/containerd/v2/internal/lazyregexp`
- `github.com/containerd/containerd/v2/internal/randutil`
- `github.com/containerd/containerd/v2/pkg/archive/compression`
- `github.com/containerd/containerd/v2/pkg/filters`
- `github.com/containerd/containerd/v2/pkg/identifiers`
- `github.com/containerd/containerd/v2/pkg/labels`
- `github.com/containerd/containerd/v2/pkg/namespaces`
- `github.com/containerd/containerd/v2/pkg/protobuf`
- `github.com/containerd/containerd/v2/pkg/protobuf/types`
- `github.com/containerd/containerd/v2/pkg/reference`
- `github.com/containerd/containerd/v2/pkg/tracing`
- `github.com/containerd/containerd/v2/plugins/content/local`
- `github.com/containerd/containerd/v2/plugins/services/content/contentserver`
- `github.com/containerd/containerd/v2/version`
- `github.com/containerd/continuity/sysx`
- `github.com/containerd/errdefs`
- `github.com/containerd/errdefs/pkg/errgrpc`
- `github.com/containerd/errdefs/pkg/internal/cause`
- `github.com/containerd/errdefs/pkg/internal/types`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/containerd/stargz-snapshotter/estargz`
- `github.com/containerd/stargz-snapshotter/estargz/errorutil`
- `github.com/containerd/ttrpc`
- `github.com/containerd/typeurl/v2`
- `github.com/cyphar/filepath-securejoin`
- `github.com/cyphar/filepath-securejoin/internal/consts`
- `github.com/davecgh/go-spew/spew`
- `github.com/distribution/reference`
- ... (1112 more)

**Stdlib deps**

- 233 packages

## `github.com/example/ktl/internal/agent`

**Internal deps**

- `github.com/example/ktl/internal/api/convert`
- `github.com/example/ktl/internal/capture`
- `github.com/example/ktl/internal/caststream`
- `github.com/example/ktl/internal/castutil`
- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/csvutil`
- `github.com/example/ktl/internal/deploy`
- `github.com/example/ktl/internal/dockerconfig`
- `github.com/example/ktl/internal/drift`
- `github.com/example/ktl/internal/kube`
- `github.com/example/ktl/internal/logging`
- `github.com/example/ktl/internal/sqlitewriter`
- `github.com/example/ktl/internal/tailer`
- `github.com/example/ktl/internal/workflows/buildsvc`
- `github.com/example/ktl/pkg/api/v1`
- `github.com/example/ktl/pkg/buildkit`
- `github.com/example/ktl/pkg/compose`
- `github.com/example/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/asaskevich/govalidator`
- `github.com/blang/semver/v4`
- `github.com/chai2010/gettext-go`
- `github.com/chai2010/gettext-go/mo`
- `github.com/chai2010/gettext-go/plural`
- `github.com/chai2010/gettext-go/po`
- `github.com/compose-spec/compose-go/v2/consts`
- `github.com/compose-spec/compose-go/v2/dotenv`
- `github.com/compose-spec/compose-go/v2/errdefs`
- `github.com/compose-spec/compose-go/v2/format`
- `github.com/compose-spec/compose-go/v2/graph`
- `github.com/compose-spec/compose-go/v2/interpolation`
- `github.com/compose-spec/compose-go/v2/loader`
- `github.com/compose-spec/compose-go/v2/override`
- `github.com/compose-spec/compose-go/v2/paths`
- `github.com/compose-spec/compose-go/v2/schema`
- `github.com/compose-spec/compose-go/v2/template`
- `github.com/compose-spec/compose-go/v2/transform`
- `github.com/compose-spec/compose-go/v2/tree`
- `github.com/compose-spec/compose-go/v2/types`
- `github.com/compose-spec/compose-go/v2/utils`
- `github.com/compose-spec/compose-go/v2/validation`
- `github.com/containerd/console`
- `github.com/containerd/containerd/api/services/content/v1`
- `github.com/containerd/containerd/archive/compression`
- `github.com/containerd/containerd/content`
- `github.com/containerd/containerd/errdefs`
- `github.com/containerd/containerd/filters`
- `github.com/containerd/containerd/images`
- `github.com/containerd/containerd/labels`
- `github.com/containerd/containerd/pkg/randutil`
- `github.com/containerd/containerd/remotes`
- `github.com/containerd/containerd/v2/core/content`
- `github.com/containerd/containerd/v2/core/content/proxy`
- `github.com/containerd/containerd/v2/core/images`
- `github.com/containerd/containerd/v2/core/leases`
- `github.com/containerd/containerd/v2/core/remotes`
- `github.com/containerd/containerd/v2/core/remotes/docker`
- `github.com/containerd/containerd/v2/core/remotes/docker/auth`
- `github.com/containerd/containerd/v2/core/remotes/errors`
- `github.com/containerd/containerd/v2/core/transfer`
- `github.com/containerd/containerd/v2/defaults`
- `github.com/containerd/containerd/v2/internal/fsverity`
- `github.com/containerd/containerd/v2/internal/lazyregexp`
- `github.com/containerd/containerd/v2/internal/randutil`
- `github.com/containerd/containerd/v2/pkg/archive/compression`
- `github.com/containerd/containerd/v2/pkg/filters`
- `github.com/containerd/containerd/v2/pkg/identifiers`
- `github.com/containerd/containerd/v2/pkg/labels`
- `github.com/containerd/containerd/v2/pkg/namespaces`
- `github.com/containerd/containerd/v2/pkg/protobuf`
- `github.com/containerd/containerd/v2/pkg/protobuf/types`
- `github.com/containerd/containerd/v2/pkg/reference`
- `github.com/containerd/containerd/v2/pkg/tracing`
- `github.com/containerd/containerd/v2/plugins/content/local`
- `github.com/containerd/containerd/v2/plugins/services/content/contentserver`
- `github.com/containerd/containerd/v2/version`
- `github.com/containerd/continuity/sysx`
- `github.com/containerd/errdefs`
- `github.com/containerd/errdefs/pkg/errgrpc`
- `github.com/containerd/errdefs/pkg/internal/cause`
- `github.com/containerd/errdefs/pkg/internal/types`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/containerd/stargz-snapshotter/estargz`
- `github.com/containerd/stargz-snapshotter/estargz/errorutil`
- `github.com/containerd/ttrpc`
- `github.com/containerd/typeurl/v2`
- `github.com/cyphar/filepath-securejoin`
- `github.com/cyphar/filepath-securejoin/internal/consts`
- `github.com/davecgh/go-spew/spew`
- `github.com/distribution/reference`
- ... (1112 more)

**Stdlib deps**

- 233 packages

## `github.com/example/ktl/internal/api/convert`

**Internal deps**

- `github.com/example/ktl/internal/capture`
- `github.com/example/ktl/internal/caststream`
- `github.com/example/ktl/internal/castutil`
- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/csvutil`
- `github.com/example/ktl/internal/deploy`
- `github.com/example/ktl/internal/dockerconfig`
- `github.com/example/ktl/internal/kube`
- `github.com/example/ktl/internal/logging`
- `github.com/example/ktl/internal/sqlitewriter`
- `github.com/example/ktl/internal/tailer`
- `github.com/example/ktl/internal/workflows/buildsvc`
- `github.com/example/ktl/pkg/api/v1`
- `github.com/example/ktl/pkg/buildkit`
- `github.com/example/ktl/pkg/compose`
- `github.com/example/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/asaskevich/govalidator`
- `github.com/blang/semver/v4`
- `github.com/chai2010/gettext-go`
- `github.com/chai2010/gettext-go/mo`
- `github.com/chai2010/gettext-go/plural`
- `github.com/chai2010/gettext-go/po`
- `github.com/compose-spec/compose-go/v2/consts`
- `github.com/compose-spec/compose-go/v2/dotenv`
- `github.com/compose-spec/compose-go/v2/errdefs`
- `github.com/compose-spec/compose-go/v2/format`
- `github.com/compose-spec/compose-go/v2/graph`
- `github.com/compose-spec/compose-go/v2/interpolation`
- `github.com/compose-spec/compose-go/v2/loader`
- `github.com/compose-spec/compose-go/v2/override`
- `github.com/compose-spec/compose-go/v2/paths`
- `github.com/compose-spec/compose-go/v2/schema`
- `github.com/compose-spec/compose-go/v2/template`
- `github.com/compose-spec/compose-go/v2/transform`
- `github.com/compose-spec/compose-go/v2/tree`
- `github.com/compose-spec/compose-go/v2/types`
- `github.com/compose-spec/compose-go/v2/utils`
- `github.com/compose-spec/compose-go/v2/validation`
- `github.com/containerd/console`
- `github.com/containerd/containerd/api/services/content/v1`
- `github.com/containerd/containerd/archive/compression`
- `github.com/containerd/containerd/content`
- `github.com/containerd/containerd/errdefs`
- `github.com/containerd/containerd/filters`
- `github.com/containerd/containerd/images`
- `github.com/containerd/containerd/labels`
- `github.com/containerd/containerd/pkg/randutil`
- `github.com/containerd/containerd/remotes`
- `github.com/containerd/containerd/v2/core/content`
- `github.com/containerd/containerd/v2/core/content/proxy`
- `github.com/containerd/containerd/v2/core/images`
- `github.com/containerd/containerd/v2/core/leases`
- `github.com/containerd/containerd/v2/core/remotes`
- `github.com/containerd/containerd/v2/core/remotes/docker`
- `github.com/containerd/containerd/v2/core/remotes/docker/auth`
- `github.com/containerd/containerd/v2/core/remotes/errors`
- `github.com/containerd/containerd/v2/core/transfer`
- `github.com/containerd/containerd/v2/defaults`
- `github.com/containerd/containerd/v2/internal/fsverity`
- `github.com/containerd/containerd/v2/internal/lazyregexp`
- `github.com/containerd/containerd/v2/internal/randutil`
- `github.com/containerd/containerd/v2/pkg/archive/compression`
- `github.com/containerd/containerd/v2/pkg/filters`
- `github.com/containerd/containerd/v2/pkg/identifiers`
- `github.com/containerd/containerd/v2/pkg/labels`
- `github.com/containerd/containerd/v2/pkg/namespaces`
- `github.com/containerd/containerd/v2/pkg/protobuf`
- `github.com/containerd/containerd/v2/pkg/protobuf/types`
- `github.com/containerd/containerd/v2/pkg/reference`
- `github.com/containerd/containerd/v2/pkg/tracing`
- `github.com/containerd/containerd/v2/plugins/content/local`
- `github.com/containerd/containerd/v2/plugins/services/content/contentserver`
- `github.com/containerd/containerd/v2/version`
- `github.com/containerd/continuity/sysx`
- `github.com/containerd/errdefs`
- `github.com/containerd/errdefs/pkg/errgrpc`
- `github.com/containerd/errdefs/pkg/internal/cause`
- `github.com/containerd/errdefs/pkg/internal/types`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/containerd/stargz-snapshotter/estargz`
- `github.com/containerd/stargz-snapshotter/estargz/errorutil`
- `github.com/containerd/ttrpc`
- `github.com/containerd/typeurl/v2`
- `github.com/cyphar/filepath-securejoin`
- `github.com/cyphar/filepath-securejoin/internal/consts`
- `github.com/davecgh/go-spew/spew`
- `github.com/distribution/reference`
- ... (1112 more)

**Stdlib deps**

- 233 packages

## `github.com/example/ktl/internal/capture`

**Internal deps**

- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/kube`
- `github.com/example/ktl/internal/sqlitewriter`
- `github.com/example/ktl/internal/tailer`

**Third-party deps**

- `github.com/davecgh/go-spew/spew`
- `github.com/dustin/go-humanize`
- `github.com/emicklei/go-restful/v3`
- `github.com/emicklei/go-restful/v3/log`
- `github.com/fatih/color`
- `github.com/fxamacker/cbor/v2`
- `github.com/go-logr/logr`
- `github.com/go-openapi/jsonpointer`
- `github.com/go-openapi/jsonreference`
- `github.com/go-openapi/jsonreference/internal`
- `github.com/go-openapi/swag`
- `github.com/gogo/protobuf/proto`
- `github.com/gogo/protobuf/sortkeys`
- `github.com/google/gnostic-models/compiler`
- `github.com/google/gnostic-models/extensions`
- `github.com/google/gnostic-models/jsonschema`
- `github.com/google/gnostic-models/openapiv2`
- `github.com/google/gnostic-models/openapiv3`
- `github.com/google/uuid`
- `github.com/gorilla/websocket`
- `github.com/josharian/intern`
- `github.com/json-iterator/go`
- `github.com/mailru/easyjson/buffer`
- `github.com/mailru/easyjson/jlexer`
- `github.com/mailru/easyjson/jwriter`
- `github.com/mattn/go-colorable`
- `github.com/mattn/go-isatty`
- `github.com/mitchellh/go-homedir`
- `github.com/moby/spdystream`
- `github.com/moby/spdystream/spdy`
- `github.com/modern-go/concurrent`
- `github.com/modern-go/reflect2`
- `github.com/munnerz/goautoneg`
- `github.com/mxk/go-flowrate/flowrate`
- `github.com/ncruces/go-strftime`
- `github.com/pkg/errors`
- `github.com/pmezard/go-difflib/difflib`
- `github.com/remyoudompheng/bigfft`
- `github.com/spf13/cobra`
- `github.com/spf13/pflag`
- `github.com/x448/float16`
- `go.yaml.in/yaml/v2`
- `go.yaml.in/yaml/v3`
- `golang.org/x/exp/constraints`
- `golang.org/x/net/html`
- `golang.org/x/net/html/atom`
- `golang.org/x/net/http/httpguts`
- `golang.org/x/net/http2`
- `golang.org/x/net/http2/hpack`
- `golang.org/x/net/idna`
- `golang.org/x/net/internal/httpcommon`
- `golang.org/x/net/internal/socks`
- `golang.org/x/net/proxy`
- `golang.org/x/net/websocket`
- `golang.org/x/oauth2`
- `golang.org/x/oauth2/internal`
- `golang.org/x/sync/errgroup`
- `golang.org/x/sys/unix`
- `golang.org/x/term`
- `golang.org/x/text/secure/bidirule`
- `golang.org/x/text/transform`
- `golang.org/x/text/unicode/bidi`
- `golang.org/x/text/unicode/norm`
- `golang.org/x/time/rate`
- `google.golang.org/protobuf/encoding/prototext`
- `google.golang.org/protobuf/encoding/protowire`
- `google.golang.org/protobuf/internal/descfmt`
- `google.golang.org/protobuf/internal/descopts`
- `google.golang.org/protobuf/internal/detrand`
- `google.golang.org/protobuf/internal/editiondefaults`
- `google.golang.org/protobuf/internal/encoding/defval`
- `google.golang.org/protobuf/internal/encoding/messageset`
- `google.golang.org/protobuf/internal/encoding/tag`
- `google.golang.org/protobuf/internal/encoding/text`
- `google.golang.org/protobuf/internal/errors`
- `google.golang.org/protobuf/internal/filedesc`
- `google.golang.org/protobuf/internal/filetype`
- `google.golang.org/protobuf/internal/flags`
- `google.golang.org/protobuf/internal/genid`
- `google.golang.org/protobuf/internal/impl`
- ... (476 more)

**Stdlib deps**

- 214 packages

## `github.com/example/ktl/internal/caststream`

**Internal deps**

- `github.com/example/ktl/internal/capture`
- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/deploy`
- `github.com/example/ktl/internal/kube`
- `github.com/example/ktl/internal/sqlitewriter`
- `github.com/example/ktl/internal/tailer`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/asaskevich/govalidator`
- `github.com/blang/semver/v4`
- `github.com/chai2010/gettext-go`
- `github.com/chai2010/gettext-go/mo`
- `github.com/chai2010/gettext-go/plural`
- `github.com/chai2010/gettext-go/po`
- `github.com/containerd/containerd/archive/compression`
- `github.com/containerd/containerd/content`
- `github.com/containerd/containerd/errdefs`
- `github.com/containerd/containerd/filters`
- `github.com/containerd/containerd/images`
- `github.com/containerd/containerd/labels`
- `github.com/containerd/containerd/pkg/randutil`
- `github.com/containerd/containerd/remotes`
- `github.com/containerd/errdefs`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/cyphar/filepath-securejoin`
- `github.com/cyphar/filepath-securejoin/internal/consts`
- `github.com/davecgh/go-spew/spew`
- `github.com/dustin/go-humanize`
- `github.com/emicklei/go-restful/v3`
- `github.com/emicklei/go-restful/v3/log`
- `github.com/evanphx/json-patch`
- `github.com/exponent-io/jsonpath`
- `github.com/fatih/color`
- `github.com/fxamacker/cbor/v2`
- `github.com/go-errors/errors`
- `github.com/go-gorp/gorp/v3`
- `github.com/go-logr/logr`
- `github.com/go-openapi/jsonpointer`
- `github.com/go-openapi/jsonreference`
- `github.com/go-openapi/jsonreference/internal`
- `github.com/go-openapi/swag`
- `github.com/gobwas/glob`
- `github.com/gobwas/glob/compiler`
- `github.com/gobwas/glob/match`
- `github.com/gobwas/glob/syntax`
- `github.com/gobwas/glob/syntax/ast`
- `github.com/gobwas/glob/syntax/lexer`
- `github.com/gobwas/glob/util/runes`
- `github.com/gobwas/glob/util/strings`
- `github.com/gogo/protobuf/proto`
- `github.com/gogo/protobuf/sortkeys`
- `github.com/google/btree`
- `github.com/google/gnostic-models/compiler`
- `github.com/google/gnostic-models/extensions`
- `github.com/google/gnostic-models/jsonschema`
- `github.com/google/gnostic-models/openapiv2`
- `github.com/google/gnostic-models/openapiv3`
- `github.com/google/uuid`
- `github.com/gorilla/websocket`
- `github.com/gosuri/uitable`
- `github.com/gosuri/uitable/util/strutil`
- `github.com/gosuri/uitable/util/wordwrap`
- `github.com/gregjones/httpcache`
- `github.com/hashicorp/errwrap`
- `github.com/hashicorp/go-multierror`
- `github.com/huandu/xstrings`
- `github.com/jmoiron/sqlx`
- `github.com/jmoiron/sqlx/reflectx`
- `github.com/josharian/intern`
- `github.com/json-iterator/go`
- `github.com/klauspost/compress`
- `github.com/klauspost/compress/fse`
- `github.com/klauspost/compress/huff0`
- `github.com/klauspost/compress/internal/le`
- `github.com/klauspost/compress/internal/snapref`
- `github.com/klauspost/compress/zstd`
- `github.com/klauspost/compress/zstd/internal/xxhash`
- `github.com/lann/builder`
- `github.com/lann/ps`
- ... (788 more)

**Stdlib deps**

- 225 packages

## `github.com/example/ktl/internal/castutil`

**Internal deps**

- `github.com/example/ktl/internal/capture`
- `github.com/example/ktl/internal/caststream`
- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/deploy`
- `github.com/example/ktl/internal/kube`
- `github.com/example/ktl/internal/sqlitewriter`
- `github.com/example/ktl/internal/tailer`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/asaskevich/govalidator`
- `github.com/blang/semver/v4`
- `github.com/chai2010/gettext-go`
- `github.com/chai2010/gettext-go/mo`
- `github.com/chai2010/gettext-go/plural`
- `github.com/chai2010/gettext-go/po`
- `github.com/containerd/containerd/archive/compression`
- `github.com/containerd/containerd/content`
- `github.com/containerd/containerd/errdefs`
- `github.com/containerd/containerd/filters`
- `github.com/containerd/containerd/images`
- `github.com/containerd/containerd/labels`
- `github.com/containerd/containerd/pkg/randutil`
- `github.com/containerd/containerd/remotes`
- `github.com/containerd/errdefs`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/cyphar/filepath-securejoin`
- `github.com/cyphar/filepath-securejoin/internal/consts`
- `github.com/davecgh/go-spew/spew`
- `github.com/dustin/go-humanize`
- `github.com/emicklei/go-restful/v3`
- `github.com/emicklei/go-restful/v3/log`
- `github.com/evanphx/json-patch`
- `github.com/exponent-io/jsonpath`
- `github.com/fatih/color`
- `github.com/fxamacker/cbor/v2`
- `github.com/go-errors/errors`
- `github.com/go-gorp/gorp/v3`
- `github.com/go-logr/logr`
- `github.com/go-openapi/jsonpointer`
- `github.com/go-openapi/jsonreference`
- `github.com/go-openapi/jsonreference/internal`
- `github.com/go-openapi/swag`
- `github.com/gobwas/glob`
- `github.com/gobwas/glob/compiler`
- `github.com/gobwas/glob/match`
- `github.com/gobwas/glob/syntax`
- `github.com/gobwas/glob/syntax/ast`
- `github.com/gobwas/glob/syntax/lexer`
- `github.com/gobwas/glob/util/runes`
- `github.com/gobwas/glob/util/strings`
- `github.com/gogo/protobuf/proto`
- `github.com/gogo/protobuf/sortkeys`
- `github.com/google/btree`
- `github.com/google/gnostic-models/compiler`
- `github.com/google/gnostic-models/extensions`
- `github.com/google/gnostic-models/jsonschema`
- `github.com/google/gnostic-models/openapiv2`
- `github.com/google/gnostic-models/openapiv3`
- `github.com/google/uuid`
- `github.com/gorilla/websocket`
- `github.com/gosuri/uitable`
- `github.com/gosuri/uitable/util/strutil`
- `github.com/gosuri/uitable/util/wordwrap`
- `github.com/gregjones/httpcache`
- `github.com/hashicorp/errwrap`
- `github.com/hashicorp/go-multierror`
- `github.com/huandu/xstrings`
- `github.com/jmoiron/sqlx`
- `github.com/jmoiron/sqlx/reflectx`
- `github.com/josharian/intern`
- `github.com/json-iterator/go`
- `github.com/klauspost/compress`
- `github.com/klauspost/compress/fse`
- `github.com/klauspost/compress/huff0`
- `github.com/klauspost/compress/internal/le`
- `github.com/klauspost/compress/internal/snapref`
- `github.com/klauspost/compress/zstd`
- `github.com/klauspost/compress/zstd/internal/xxhash`
- `github.com/lann/builder`
- `github.com/lann/ps`
- ... (788 more)

**Stdlib deps**

- 225 packages

## `github.com/example/ktl/internal/config`

**Internal deps**

- (none)

**Third-party deps**

- `github.com/fxamacker/cbor/v2`
- `github.com/go-logr/logr`
- `github.com/gogo/protobuf/proto`
- `github.com/gogo/protobuf/sortkeys`
- `github.com/json-iterator/go`
- `github.com/modern-go/concurrent`
- `github.com/modern-go/reflect2`
- `github.com/spf13/cobra`
- `github.com/spf13/pflag`
- `github.com/x448/float16`
- `go.yaml.in/yaml/v2`
- `golang.org/x/net/http/httpguts`
- `golang.org/x/net/http2`
- `golang.org/x/net/http2/hpack`
- `golang.org/x/net/idna`
- `golang.org/x/net/internal/httpcommon`
- `golang.org/x/text/secure/bidirule`
- `golang.org/x/text/transform`
- `golang.org/x/text/unicode/bidi`
- `golang.org/x/text/unicode/norm`
- `gopkg.in/inf.v0`
- `k8s.io/api/core/v1`
- `k8s.io/apimachinery/pkg/api/operation`
- `k8s.io/apimachinery/pkg/api/resource`
- `k8s.io/apimachinery/pkg/apis/meta/v1`
- `k8s.io/apimachinery/pkg/conversion`
- `k8s.io/apimachinery/pkg/conversion/queryparams`
- `k8s.io/apimachinery/pkg/fields`
- `k8s.io/apimachinery/pkg/labels`
- `k8s.io/apimachinery/pkg/runtime`
- `k8s.io/apimachinery/pkg/runtime/schema`
- `k8s.io/apimachinery/pkg/runtime/serializer/cbor/direct`
- `k8s.io/apimachinery/pkg/runtime/serializer/cbor/internal/modes`
- `k8s.io/apimachinery/pkg/selection`
- `k8s.io/apimachinery/pkg/types`
- `k8s.io/apimachinery/pkg/util/errors`
- `k8s.io/apimachinery/pkg/util/intstr`
- `k8s.io/apimachinery/pkg/util/json`
- `k8s.io/apimachinery/pkg/util/naming`
- `k8s.io/apimachinery/pkg/util/net`
- `k8s.io/apimachinery/pkg/util/runtime`
- `k8s.io/apimachinery/pkg/util/sets`
- `k8s.io/apimachinery/pkg/util/validation`
- `k8s.io/apimachinery/pkg/util/validation/field`
- `k8s.io/apimachinery/pkg/watch`
- `k8s.io/apimachinery/third_party/forked/golang/reflect`
- `k8s.io/klog/v2`
- `k8s.io/klog/v2/internal/buffer`
- `k8s.io/klog/v2/internal/clock`
- `k8s.io/klog/v2/internal/dbg`
- `k8s.io/klog/v2/internal/serialize`
- `k8s.io/klog/v2/internal/severity`
- `k8s.io/klog/v2/internal/sloghandler`
- `k8s.io/utils/internal/third_party/forked/golang/net`
- `k8s.io/utils/net`
- `k8s.io/utils/ptr`
- `sigs.k8s.io/json`
- `sigs.k8s.io/json/internal/golang/encoding/json`
- `sigs.k8s.io/randfill`
- `sigs.k8s.io/randfill/bytesource`
- `sigs.k8s.io/structured-merge-diff/v6/value`

**Stdlib deps**

- 201 packages

## `github.com/example/ktl/internal/csvutil`

**Internal deps**

- (none)

**Third-party deps**

- (none)

**Stdlib deps**

- 62 packages

## `github.com/example/ktl/internal/deploy`

**Internal deps**

- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/kube`
- `github.com/example/ktl/internal/sqlitewriter`
- `github.com/example/ktl/internal/tailer`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/asaskevich/govalidator`
- `github.com/blang/semver/v4`
- `github.com/chai2010/gettext-go`
- `github.com/chai2010/gettext-go/mo`
- `github.com/chai2010/gettext-go/plural`
- `github.com/chai2010/gettext-go/po`
- `github.com/containerd/containerd/archive/compression`
- `github.com/containerd/containerd/content`
- `github.com/containerd/containerd/errdefs`
- `github.com/containerd/containerd/filters`
- `github.com/containerd/containerd/images`
- `github.com/containerd/containerd/labels`
- `github.com/containerd/containerd/pkg/randutil`
- `github.com/containerd/containerd/remotes`
- `github.com/containerd/errdefs`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/cyphar/filepath-securejoin`
- `github.com/cyphar/filepath-securejoin/internal/consts`
- `github.com/davecgh/go-spew/spew`
- `github.com/dustin/go-humanize`
- `github.com/emicklei/go-restful/v3`
- `github.com/emicklei/go-restful/v3/log`
- `github.com/evanphx/json-patch`
- `github.com/exponent-io/jsonpath`
- `github.com/fatih/color`
- `github.com/fxamacker/cbor/v2`
- `github.com/go-errors/errors`
- `github.com/go-gorp/gorp/v3`
- `github.com/go-logr/logr`
- `github.com/go-openapi/jsonpointer`
- `github.com/go-openapi/jsonreference`
- `github.com/go-openapi/jsonreference/internal`
- `github.com/go-openapi/swag`
- `github.com/gobwas/glob`
- `github.com/gobwas/glob/compiler`
- `github.com/gobwas/glob/match`
- `github.com/gobwas/glob/syntax`
- `github.com/gobwas/glob/syntax/ast`
- `github.com/gobwas/glob/syntax/lexer`
- `github.com/gobwas/glob/util/runes`
- `github.com/gobwas/glob/util/strings`
- `github.com/gogo/protobuf/proto`
- `github.com/gogo/protobuf/sortkeys`
- `github.com/google/btree`
- `github.com/google/gnostic-models/compiler`
- `github.com/google/gnostic-models/extensions`
- `github.com/google/gnostic-models/jsonschema`
- `github.com/google/gnostic-models/openapiv2`
- `github.com/google/gnostic-models/openapiv3`
- `github.com/google/uuid`
- `github.com/gorilla/websocket`
- `github.com/gosuri/uitable`
- `github.com/gosuri/uitable/util/strutil`
- `github.com/gosuri/uitable/util/wordwrap`
- `github.com/gregjones/httpcache`
- `github.com/hashicorp/errwrap`
- `github.com/hashicorp/go-multierror`
- `github.com/huandu/xstrings`
- `github.com/jmoiron/sqlx`
- `github.com/jmoiron/sqlx/reflectx`
- `github.com/josharian/intern`
- `github.com/json-iterator/go`
- `github.com/klauspost/compress`
- `github.com/klauspost/compress/fse`
- `github.com/klauspost/compress/huff0`
- `github.com/klauspost/compress/internal/le`
- `github.com/klauspost/compress/internal/snapref`
- `github.com/klauspost/compress/zstd`
- `github.com/klauspost/compress/zstd/internal/xxhash`
- `github.com/lann/builder`
- `github.com/lann/ps`
- ... (665 more)

**Stdlib deps**

- 225 packages

## `github.com/example/ktl/internal/dockerconfig`

**Internal deps**

- (none)

**Third-party deps**

- `github.com/docker/cli/cli/config`
- `github.com/docker/cli/cli/config/configfile`
- `github.com/docker/cli/cli/config/credentials`
- `github.com/docker/cli/cli/config/memorystore`
- `github.com/docker/cli/cli/config/types`
- `github.com/docker/docker-credential-helpers/client`
- `github.com/docker/docker-credential-helpers/credentials`
- `github.com/pkg/errors`
- `github.com/sirupsen/logrus`
- `golang.org/x/sys/unix`

**Stdlib deps**

- 82 packages

## `github.com/example/ktl/internal/drift`

**Internal deps**

- (none)

**Third-party deps**

- `github.com/davecgh/go-spew/spew`
- `github.com/emicklei/go-restful/v3`
- `github.com/emicklei/go-restful/v3/log`
- `github.com/fxamacker/cbor/v2`
- `github.com/go-logr/logr`
- `github.com/go-openapi/jsonpointer`
- `github.com/go-openapi/jsonreference`
- `github.com/go-openapi/jsonreference/internal`
- `github.com/go-openapi/swag`
- `github.com/gogo/protobuf/proto`
- `github.com/gogo/protobuf/sortkeys`
- `github.com/google/gnostic-models/compiler`
- `github.com/google/gnostic-models/extensions`
- `github.com/google/gnostic-models/jsonschema`
- `github.com/google/gnostic-models/openapiv2`
- `github.com/google/gnostic-models/openapiv3`
- `github.com/google/uuid`
- `github.com/josharian/intern`
- `github.com/json-iterator/go`
- `github.com/mailru/easyjson/buffer`
- `github.com/mailru/easyjson/jlexer`
- `github.com/mailru/easyjson/jwriter`
- `github.com/modern-go/concurrent`
- `github.com/modern-go/reflect2`
- `github.com/munnerz/goautoneg`
- `github.com/pkg/errors`
- `github.com/x448/float16`
- `go.yaml.in/yaml/v2`
- `go.yaml.in/yaml/v3`
- `golang.org/x/net/http/httpguts`
- `golang.org/x/net/http2`
- `golang.org/x/net/http2/hpack`
- `golang.org/x/net/idna`
- `golang.org/x/net/internal/httpcommon`
- `golang.org/x/oauth2`
- `golang.org/x/oauth2/internal`
- `golang.org/x/sys/unix`
- `golang.org/x/term`
- `golang.org/x/text/secure/bidirule`
- `golang.org/x/text/transform`
- `golang.org/x/text/unicode/bidi`
- `golang.org/x/text/unicode/norm`
- `golang.org/x/time/rate`
- `google.golang.org/protobuf/encoding/prototext`
- `google.golang.org/protobuf/encoding/protowire`
- `google.golang.org/protobuf/internal/descfmt`
- `google.golang.org/protobuf/internal/descopts`
- `google.golang.org/protobuf/internal/detrand`
- `google.golang.org/protobuf/internal/editiondefaults`
- `google.golang.org/protobuf/internal/encoding/defval`
- `google.golang.org/protobuf/internal/encoding/messageset`
- `google.golang.org/protobuf/internal/encoding/tag`
- `google.golang.org/protobuf/internal/encoding/text`
- `google.golang.org/protobuf/internal/errors`
- `google.golang.org/protobuf/internal/filedesc`
- `google.golang.org/protobuf/internal/filetype`
- `google.golang.org/protobuf/internal/flags`
- `google.golang.org/protobuf/internal/genid`
- `google.golang.org/protobuf/internal/impl`
- `google.golang.org/protobuf/internal/order`
- `google.golang.org/protobuf/internal/pragma`
- `google.golang.org/protobuf/internal/protolazy`
- `google.golang.org/protobuf/internal/set`
- `google.golang.org/protobuf/internal/strs`
- `google.golang.org/protobuf/internal/version`
- `google.golang.org/protobuf/proto`
- `google.golang.org/protobuf/reflect/protoreflect`
- `google.golang.org/protobuf/reflect/protoregistry`
- `google.golang.org/protobuf/runtime/protoiface`
- `google.golang.org/protobuf/runtime/protoimpl`
- `google.golang.org/protobuf/types/descriptorpb`
- `google.golang.org/protobuf/types/known/anypb`
- `gopkg.in/evanphx/json-patch.v4`
- `gopkg.in/inf.v0`
- `gopkg.in/yaml.v3`
- `k8s.io/api/admissionregistration/v1`
- `k8s.io/api/admissionregistration/v1alpha1`
- `k8s.io/api/admissionregistration/v1beta1`
- `k8s.io/api/apidiscovery/v2`
- `k8s.io/api/apidiscovery/v2beta1`
- ... (266 more)

**Stdlib deps**

- 206 packages

## `github.com/example/ktl/internal/featureflags`

**Internal deps**

- (none)

**Third-party deps**

- (none)

**Stdlib deps**

- 61 packages

## `github.com/example/ktl/internal/grpcutil`

**Internal deps**

- (none)

**Third-party deps**

- `golang.org/x/net/http/httpguts`
- `golang.org/x/net/http2`
- `golang.org/x/net/http2/hpack`
- `golang.org/x/net/idna`
- `golang.org/x/net/internal/httpcommon`
- `golang.org/x/net/internal/timeseries`
- `golang.org/x/net/trace`
- `golang.org/x/sys/unix`
- `golang.org/x/text/secure/bidirule`
- `golang.org/x/text/transform`
- `golang.org/x/text/unicode/bidi`
- `golang.org/x/text/unicode/norm`
- `google.golang.org/genproto/googleapis/rpc/status`
- `google.golang.org/grpc`
- `google.golang.org/grpc/attributes`
- `google.golang.org/grpc/backoff`
- `google.golang.org/grpc/balancer`
- `google.golang.org/grpc/balancer/base`
- `google.golang.org/grpc/balancer/endpointsharding`
- `google.golang.org/grpc/balancer/grpclb/state`
- `google.golang.org/grpc/balancer/pickfirst`
- `google.golang.org/grpc/balancer/pickfirst/internal`
- `google.golang.org/grpc/balancer/pickfirst/pickfirstleaf`
- `google.golang.org/grpc/balancer/roundrobin`
- `google.golang.org/grpc/binarylog/grpc_binarylog_v1`
- `google.golang.org/grpc/channelz`
- `google.golang.org/grpc/codes`
- `google.golang.org/grpc/connectivity`
- `google.golang.org/grpc/credentials`
- `google.golang.org/grpc/credentials/insecure`
- `google.golang.org/grpc/encoding`
- `google.golang.org/grpc/encoding/proto`
- `google.golang.org/grpc/experimental/stats`
- `google.golang.org/grpc/grpclog`
- `google.golang.org/grpc/grpclog/internal`
- `google.golang.org/grpc/internal`
- `google.golang.org/grpc/internal/backoff`
- `google.golang.org/grpc/internal/balancer/gracefulswitch`
- `google.golang.org/grpc/internal/balancerload`
- `google.golang.org/grpc/internal/binarylog`
- `google.golang.org/grpc/internal/buffer`
- `google.golang.org/grpc/internal/channelz`
- `google.golang.org/grpc/internal/credentials`
- `google.golang.org/grpc/internal/envconfig`
- `google.golang.org/grpc/internal/grpclog`
- `google.golang.org/grpc/internal/grpcsync`
- `google.golang.org/grpc/internal/grpcutil`
- `google.golang.org/grpc/internal/idle`
- `google.golang.org/grpc/internal/metadata`
- `google.golang.org/grpc/internal/pretty`
- `google.golang.org/grpc/internal/proxyattributes`
- `google.golang.org/grpc/internal/resolver`
- `google.golang.org/grpc/internal/resolver/delegatingresolver`
- `google.golang.org/grpc/internal/resolver/dns`
- `google.golang.org/grpc/internal/resolver/dns/internal`
- `google.golang.org/grpc/internal/resolver/passthrough`
- `google.golang.org/grpc/internal/resolver/unix`
- `google.golang.org/grpc/internal/serviceconfig`
- `google.golang.org/grpc/internal/stats`
- `google.golang.org/grpc/internal/status`
- `google.golang.org/grpc/internal/syscall`
- `google.golang.org/grpc/internal/transport`
- `google.golang.org/grpc/internal/transport/networktype`
- `google.golang.org/grpc/keepalive`
- `google.golang.org/grpc/mem`
- `google.golang.org/grpc/metadata`
- `google.golang.org/grpc/peer`
- `google.golang.org/grpc/resolver`
- `google.golang.org/grpc/resolver/dns`
- `google.golang.org/grpc/serviceconfig`
- `google.golang.org/grpc/stats`
- `google.golang.org/grpc/status`
- `google.golang.org/grpc/tap`
- `google.golang.org/protobuf/encoding/protojson`
- `google.golang.org/protobuf/encoding/prototext`
- `google.golang.org/protobuf/encoding/protowire`
- `google.golang.org/protobuf/internal/descfmt`
- `google.golang.org/protobuf/internal/descopts`
- `google.golang.org/protobuf/internal/detrand`
- `google.golang.org/protobuf/internal/editiondefaults`
- ... (26 more)

**Stdlib deps**

- 191 packages

## `github.com/example/ktl/internal/kube`

**Internal deps**

- (none)

**Third-party deps**

- `github.com/davecgh/go-spew/spew`
- `github.com/emicklei/go-restful/v3`
- `github.com/emicklei/go-restful/v3/log`
- `github.com/fxamacker/cbor/v2`
- `github.com/go-logr/logr`
- `github.com/go-openapi/jsonpointer`
- `github.com/go-openapi/jsonreference`
- `github.com/go-openapi/jsonreference/internal`
- `github.com/go-openapi/swag`
- `github.com/gogo/protobuf/proto`
- `github.com/gogo/protobuf/sortkeys`
- `github.com/google/gnostic-models/compiler`
- `github.com/google/gnostic-models/extensions`
- `github.com/google/gnostic-models/jsonschema`
- `github.com/google/gnostic-models/openapiv2`
- `github.com/google/gnostic-models/openapiv3`
- `github.com/google/uuid`
- `github.com/gorilla/websocket`
- `github.com/josharian/intern`
- `github.com/json-iterator/go`
- `github.com/mailru/easyjson/buffer`
- `github.com/mailru/easyjson/jlexer`
- `github.com/mailru/easyjson/jwriter`
- `github.com/mitchellh/go-homedir`
- `github.com/moby/spdystream`
- `github.com/moby/spdystream/spdy`
- `github.com/modern-go/concurrent`
- `github.com/modern-go/reflect2`
- `github.com/munnerz/goautoneg`
- `github.com/mxk/go-flowrate/flowrate`
- `github.com/pkg/errors`
- `github.com/spf13/pflag`
- `github.com/x448/float16`
- `go.yaml.in/yaml/v2`
- `go.yaml.in/yaml/v3`
- `golang.org/x/net/html`
- `golang.org/x/net/html/atom`
- `golang.org/x/net/http/httpguts`
- `golang.org/x/net/http2`
- `golang.org/x/net/http2/hpack`
- `golang.org/x/net/idna`
- `golang.org/x/net/internal/httpcommon`
- `golang.org/x/net/internal/socks`
- `golang.org/x/net/proxy`
- `golang.org/x/net/websocket`
- `golang.org/x/oauth2`
- `golang.org/x/oauth2/internal`
- `golang.org/x/sys/unix`
- `golang.org/x/term`
- `golang.org/x/text/secure/bidirule`
- `golang.org/x/text/transform`
- `golang.org/x/text/unicode/bidi`
- `golang.org/x/text/unicode/norm`
- `golang.org/x/time/rate`
- `google.golang.org/protobuf/encoding/prototext`
- `google.golang.org/protobuf/encoding/protowire`
- `google.golang.org/protobuf/internal/descfmt`
- `google.golang.org/protobuf/internal/descopts`
- `google.golang.org/protobuf/internal/detrand`
- `google.golang.org/protobuf/internal/editiondefaults`
- `google.golang.org/protobuf/internal/encoding/defval`
- `google.golang.org/protobuf/internal/encoding/messageset`
- `google.golang.org/protobuf/internal/encoding/tag`
- `google.golang.org/protobuf/internal/encoding/text`
- `google.golang.org/protobuf/internal/errors`
- `google.golang.org/protobuf/internal/filedesc`
- `google.golang.org/protobuf/internal/filetype`
- `google.golang.org/protobuf/internal/flags`
- `google.golang.org/protobuf/internal/genid`
- `google.golang.org/protobuf/internal/impl`
- `google.golang.org/protobuf/internal/order`
- `google.golang.org/protobuf/internal/pragma`
- `google.golang.org/protobuf/internal/protolazy`
- `google.golang.org/protobuf/internal/set`
- `google.golang.org/protobuf/internal/strs`
- `google.golang.org/protobuf/internal/version`
- `google.golang.org/protobuf/proto`
- `google.golang.org/protobuf/reflect/protoreflect`
- `google.golang.org/protobuf/reflect/protoregistry`
- `google.golang.org/protobuf/runtime/protoiface`
- ... (304 more)

**Stdlib deps**

- 208 packages

## `github.com/example/ktl/internal/logging`

**Internal deps**

- (none)

**Third-party deps**

- `github.com/fxamacker/cbor/v2`
- `github.com/go-logr/logr`
- `github.com/go-logr/logr/slogr`
- `github.com/go-logr/zapr`
- `github.com/gogo/protobuf/proto`
- `github.com/gogo/protobuf/sortkeys`
- `github.com/json-iterator/go`
- `github.com/modern-go/concurrent`
- `github.com/modern-go/reflect2`
- `github.com/x448/float16`
- `go.uber.org/multierr`
- `go.uber.org/zap`
- `go.uber.org/zap/buffer`
- `go.uber.org/zap/internal`
- `go.uber.org/zap/internal/bufferpool`
- `go.uber.org/zap/internal/color`
- `go.uber.org/zap/internal/exit`
- `go.uber.org/zap/internal/pool`
- `go.uber.org/zap/internal/stacktrace`
- `go.uber.org/zap/zapcore`
- `go.yaml.in/yaml/v2`
- `golang.org/x/net/http/httpguts`
- `golang.org/x/net/http2`
- `golang.org/x/net/http2/hpack`
- `golang.org/x/net/idna`
- `golang.org/x/net/internal/httpcommon`
- `golang.org/x/text/secure/bidirule`
- `golang.org/x/text/transform`
- `golang.org/x/text/unicode/bidi`
- `golang.org/x/text/unicode/norm`
- `gopkg.in/inf.v0`
- `k8s.io/apimachinery/pkg/api/meta`
- `k8s.io/apimachinery/pkg/api/operation`
- `k8s.io/apimachinery/pkg/api/resource`
- `k8s.io/apimachinery/pkg/apis/meta/v1`
- `k8s.io/apimachinery/pkg/conversion`
- `k8s.io/apimachinery/pkg/conversion/queryparams`
- `k8s.io/apimachinery/pkg/fields`
- `k8s.io/apimachinery/pkg/labels`
- `k8s.io/apimachinery/pkg/runtime`
- `k8s.io/apimachinery/pkg/runtime/schema`
- `k8s.io/apimachinery/pkg/runtime/serializer/cbor/direct`
- `k8s.io/apimachinery/pkg/runtime/serializer/cbor/internal/modes`
- `k8s.io/apimachinery/pkg/selection`
- `k8s.io/apimachinery/pkg/types`
- `k8s.io/apimachinery/pkg/util/errors`
- `k8s.io/apimachinery/pkg/util/intstr`
- `k8s.io/apimachinery/pkg/util/json`
- `k8s.io/apimachinery/pkg/util/naming`
- `k8s.io/apimachinery/pkg/util/net`
- `k8s.io/apimachinery/pkg/util/runtime`
- `k8s.io/apimachinery/pkg/util/sets`
- `k8s.io/apimachinery/pkg/util/validation`
- `k8s.io/apimachinery/pkg/util/validation/field`
- `k8s.io/apimachinery/pkg/watch`
- `k8s.io/apimachinery/third_party/forked/golang/reflect`
- `k8s.io/klog/v2`
- `k8s.io/klog/v2/internal/buffer`
- `k8s.io/klog/v2/internal/clock`
- `k8s.io/klog/v2/internal/dbg`
- `k8s.io/klog/v2/internal/serialize`
- `k8s.io/klog/v2/internal/severity`
- `k8s.io/klog/v2/internal/sloghandler`
- `k8s.io/utils/internal/third_party/forked/golang/net`
- `k8s.io/utils/net`
- `k8s.io/utils/ptr`
- `sigs.k8s.io/controller-runtime/pkg/log/zap`
- `sigs.k8s.io/json`
- `sigs.k8s.io/json/internal/golang/encoding/json`
- `sigs.k8s.io/randfill`
- `sigs.k8s.io/randfill/bytesource`
- `sigs.k8s.io/structured-merge-diff/v6/value`

**Stdlib deps**

- 198 packages

## `github.com/example/ktl/internal/mirrorbus`

**Internal deps**

- `github.com/example/ktl/internal/api/convert`
- `github.com/example/ktl/internal/capture`
- `github.com/example/ktl/internal/caststream`
- `github.com/example/ktl/internal/castutil`
- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/csvutil`
- `github.com/example/ktl/internal/deploy`
- `github.com/example/ktl/internal/dockerconfig`
- `github.com/example/ktl/internal/grpcutil`
- `github.com/example/ktl/internal/kube`
- `github.com/example/ktl/internal/logging`
- `github.com/example/ktl/internal/sqlitewriter`
- `github.com/example/ktl/internal/tailer`
- `github.com/example/ktl/internal/workflows/buildsvc`
- `github.com/example/ktl/pkg/api/v1`
- `github.com/example/ktl/pkg/buildkit`
- `github.com/example/ktl/pkg/compose`
- `github.com/example/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/asaskevich/govalidator`
- `github.com/blang/semver/v4`
- `github.com/chai2010/gettext-go`
- `github.com/chai2010/gettext-go/mo`
- `github.com/chai2010/gettext-go/plural`
- `github.com/chai2010/gettext-go/po`
- `github.com/compose-spec/compose-go/v2/consts`
- `github.com/compose-spec/compose-go/v2/dotenv`
- `github.com/compose-spec/compose-go/v2/errdefs`
- `github.com/compose-spec/compose-go/v2/format`
- `github.com/compose-spec/compose-go/v2/graph`
- `github.com/compose-spec/compose-go/v2/interpolation`
- `github.com/compose-spec/compose-go/v2/loader`
- `github.com/compose-spec/compose-go/v2/override`
- `github.com/compose-spec/compose-go/v2/paths`
- `github.com/compose-spec/compose-go/v2/schema`
- `github.com/compose-spec/compose-go/v2/template`
- `github.com/compose-spec/compose-go/v2/transform`
- `github.com/compose-spec/compose-go/v2/tree`
- `github.com/compose-spec/compose-go/v2/types`
- `github.com/compose-spec/compose-go/v2/utils`
- `github.com/compose-spec/compose-go/v2/validation`
- `github.com/containerd/console`
- `github.com/containerd/containerd/api/services/content/v1`
- `github.com/containerd/containerd/archive/compression`
- `github.com/containerd/containerd/content`
- `github.com/containerd/containerd/errdefs`
- `github.com/containerd/containerd/filters`
- `github.com/containerd/containerd/images`
- `github.com/containerd/containerd/labels`
- `github.com/containerd/containerd/pkg/randutil`
- `github.com/containerd/containerd/remotes`
- `github.com/containerd/containerd/v2/core/content`
- `github.com/containerd/containerd/v2/core/content/proxy`
- `github.com/containerd/containerd/v2/core/images`
- `github.com/containerd/containerd/v2/core/leases`
- `github.com/containerd/containerd/v2/core/remotes`
- `github.com/containerd/containerd/v2/core/remotes/docker`
- `github.com/containerd/containerd/v2/core/remotes/docker/auth`
- `github.com/containerd/containerd/v2/core/remotes/errors`
- `github.com/containerd/containerd/v2/core/transfer`
- `github.com/containerd/containerd/v2/defaults`
- `github.com/containerd/containerd/v2/internal/fsverity`
- `github.com/containerd/containerd/v2/internal/lazyregexp`
- `github.com/containerd/containerd/v2/internal/randutil`
- `github.com/containerd/containerd/v2/pkg/archive/compression`
- `github.com/containerd/containerd/v2/pkg/filters`
- `github.com/containerd/containerd/v2/pkg/identifiers`
- `github.com/containerd/containerd/v2/pkg/labels`
- `github.com/containerd/containerd/v2/pkg/namespaces`
- `github.com/containerd/containerd/v2/pkg/protobuf`
- `github.com/containerd/containerd/v2/pkg/protobuf/types`
- `github.com/containerd/containerd/v2/pkg/reference`
- `github.com/containerd/containerd/v2/pkg/tracing`
- `github.com/containerd/containerd/v2/plugins/content/local`
- `github.com/containerd/containerd/v2/plugins/services/content/contentserver`
- `github.com/containerd/containerd/v2/version`
- `github.com/containerd/continuity/sysx`
- `github.com/containerd/errdefs`
- `github.com/containerd/errdefs/pkg/errgrpc`
- `github.com/containerd/errdefs/pkg/internal/cause`
- `github.com/containerd/errdefs/pkg/internal/types`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/containerd/stargz-snapshotter/estargz`
- `github.com/containerd/stargz-snapshotter/estargz/errorutil`
- `github.com/containerd/ttrpc`
- `github.com/containerd/typeurl/v2`
- `github.com/cyphar/filepath-securejoin`
- `github.com/cyphar/filepath-securejoin/internal/consts`
- `github.com/davecgh/go-spew/spew`
- `github.com/distribution/reference`
- ... (1112 more)

**Stdlib deps**

- 233 packages

## `github.com/example/ktl/internal/sqlitewriter`

**Internal deps**

- (none)

**Third-party deps**

- `github.com/dustin/go-humanize`
- `github.com/google/uuid`
- `github.com/mattn/go-isatty`
- `github.com/ncruces/go-strftime`
- `github.com/remyoudompheng/bigfft`
- `golang.org/x/exp/constraints`
- `golang.org/x/sys/unix`
- `modernc.org/libc`
- `modernc.org/libc/errno`
- `modernc.org/libc/fcntl`
- `modernc.org/libc/fts`
- `modernc.org/libc/grp`
- `modernc.org/libc/honnef.co/go/netdb`
- `modernc.org/libc/langinfo`
- `modernc.org/libc/limits`
- `modernc.org/libc/netdb`
- `modernc.org/libc/netinet/in`
- `modernc.org/libc/poll`
- `modernc.org/libc/pthread`
- `modernc.org/libc/pwd`
- `modernc.org/libc/signal`
- `modernc.org/libc/stdio`
- `modernc.org/libc/stdlib`
- `modernc.org/libc/sys/socket`
- `modernc.org/libc/sys/stat`
- `modernc.org/libc/sys/types`
- `modernc.org/libc/termios`
- `modernc.org/libc/time`
- `modernc.org/libc/unistd`
- `modernc.org/libc/utime`
- `modernc.org/libc/uuid/uuid`
- `modernc.org/libc/wctype`
- `modernc.org/mathutil`
- `modernc.org/memory`
- `modernc.org/sqlite`
- `modernc.org/sqlite/lib`

**Stdlib deps**

- 119 packages

## `github.com/example/ktl/internal/tailer`

**Internal deps**

- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/sqlitewriter`

**Third-party deps**

- `github.com/davecgh/go-spew/spew`
- `github.com/dustin/go-humanize`
- `github.com/emicklei/go-restful/v3`
- `github.com/emicklei/go-restful/v3/log`
- `github.com/fatih/color`
- `github.com/fxamacker/cbor/v2`
- `github.com/go-logr/logr`
- `github.com/go-openapi/jsonpointer`
- `github.com/go-openapi/jsonreference`
- `github.com/go-openapi/jsonreference/internal`
- `github.com/go-openapi/swag`
- `github.com/gogo/protobuf/proto`
- `github.com/gogo/protobuf/sortkeys`
- `github.com/google/gnostic-models/compiler`
- `github.com/google/gnostic-models/extensions`
- `github.com/google/gnostic-models/jsonschema`
- `github.com/google/gnostic-models/openapiv2`
- `github.com/google/gnostic-models/openapiv3`
- `github.com/google/uuid`
- `github.com/josharian/intern`
- `github.com/json-iterator/go`
- `github.com/mailru/easyjson/buffer`
- `github.com/mailru/easyjson/jlexer`
- `github.com/mailru/easyjson/jwriter`
- `github.com/mattn/go-colorable`
- `github.com/mattn/go-isatty`
- `github.com/modern-go/concurrent`
- `github.com/modern-go/reflect2`
- `github.com/munnerz/goautoneg`
- `github.com/ncruces/go-strftime`
- `github.com/pkg/errors`
- `github.com/pmezard/go-difflib/difflib`
- `github.com/remyoudompheng/bigfft`
- `github.com/spf13/cobra`
- `github.com/spf13/pflag`
- `github.com/x448/float16`
- `go.yaml.in/yaml/v2`
- `go.yaml.in/yaml/v3`
- `golang.org/x/exp/constraints`
- `golang.org/x/net/http/httpguts`
- `golang.org/x/net/http2`
- `golang.org/x/net/http2/hpack`
- `golang.org/x/net/idna`
- `golang.org/x/net/internal/httpcommon`
- `golang.org/x/oauth2`
- `golang.org/x/oauth2/internal`
- `golang.org/x/sync/errgroup`
- `golang.org/x/sys/unix`
- `golang.org/x/term`
- `golang.org/x/text/secure/bidirule`
- `golang.org/x/text/transform`
- `golang.org/x/text/unicode/bidi`
- `golang.org/x/text/unicode/norm`
- `golang.org/x/time/rate`
- `google.golang.org/protobuf/encoding/prototext`
- `google.golang.org/protobuf/encoding/protowire`
- `google.golang.org/protobuf/internal/descfmt`
- `google.golang.org/protobuf/internal/descopts`
- `google.golang.org/protobuf/internal/detrand`
- `google.golang.org/protobuf/internal/editiondefaults`
- `google.golang.org/protobuf/internal/encoding/defval`
- `google.golang.org/protobuf/internal/encoding/messageset`
- `google.golang.org/protobuf/internal/encoding/tag`
- `google.golang.org/protobuf/internal/encoding/text`
- `google.golang.org/protobuf/internal/errors`
- `google.golang.org/protobuf/internal/filedesc`
- `google.golang.org/protobuf/internal/filetype`
- `google.golang.org/protobuf/internal/flags`
- `google.golang.org/protobuf/internal/genid`
- `google.golang.org/protobuf/internal/impl`
- `google.golang.org/protobuf/internal/order`
- `google.golang.org/protobuf/internal/pragma`
- `google.golang.org/protobuf/internal/protolazy`
- `google.golang.org/protobuf/internal/set`
- `google.golang.org/protobuf/internal/strs`
- `google.golang.org/protobuf/internal/version`
- `google.golang.org/protobuf/proto`
- `google.golang.org/protobuf/reflect/protoreflect`
- `google.golang.org/protobuf/reflect/protoregistry`
- `google.golang.org/protobuf/runtime/protoiface`
- ... (316 more)

**Stdlib deps**

- 212 packages

## `github.com/example/ktl/internal/ui`

**Internal deps**

- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/deploy`
- `github.com/example/ktl/internal/kube`
- `github.com/example/ktl/internal/sqlitewriter`
- `github.com/example/ktl/internal/tailer`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/asaskevich/govalidator`
- `github.com/blang/semver/v4`
- `github.com/chai2010/gettext-go`
- `github.com/chai2010/gettext-go/mo`
- `github.com/chai2010/gettext-go/plural`
- `github.com/chai2010/gettext-go/po`
- `github.com/containerd/containerd/archive/compression`
- `github.com/containerd/containerd/content`
- `github.com/containerd/containerd/errdefs`
- `github.com/containerd/containerd/filters`
- `github.com/containerd/containerd/images`
- `github.com/containerd/containerd/labels`
- `github.com/containerd/containerd/pkg/randutil`
- `github.com/containerd/containerd/remotes`
- `github.com/containerd/errdefs`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/cyphar/filepath-securejoin`
- `github.com/cyphar/filepath-securejoin/internal/consts`
- `github.com/davecgh/go-spew/spew`
- `github.com/dustin/go-humanize`
- `github.com/emicklei/go-restful/v3`
- `github.com/emicklei/go-restful/v3/log`
- `github.com/evanphx/json-patch`
- `github.com/exponent-io/jsonpath`
- `github.com/fatih/color`
- `github.com/fxamacker/cbor/v2`
- `github.com/go-errors/errors`
- `github.com/go-gorp/gorp/v3`
- `github.com/go-logr/logr`
- `github.com/go-openapi/jsonpointer`
- `github.com/go-openapi/jsonreference`
- `github.com/go-openapi/jsonreference/internal`
- `github.com/go-openapi/swag`
- `github.com/gobwas/glob`
- `github.com/gobwas/glob/compiler`
- `github.com/gobwas/glob/match`
- `github.com/gobwas/glob/syntax`
- `github.com/gobwas/glob/syntax/ast`
- `github.com/gobwas/glob/syntax/lexer`
- `github.com/gobwas/glob/util/runes`
- `github.com/gobwas/glob/util/strings`
- `github.com/gogo/protobuf/proto`
- `github.com/gogo/protobuf/sortkeys`
- `github.com/google/btree`
- `github.com/google/gnostic-models/compiler`
- `github.com/google/gnostic-models/extensions`
- `github.com/google/gnostic-models/jsonschema`
- `github.com/google/gnostic-models/openapiv2`
- `github.com/google/gnostic-models/openapiv3`
- `github.com/google/uuid`
- `github.com/gorilla/websocket`
- `github.com/gosuri/uitable`
- `github.com/gosuri/uitable/util/strutil`
- `github.com/gosuri/uitable/util/wordwrap`
- `github.com/gregjones/httpcache`
- `github.com/hashicorp/errwrap`
- `github.com/hashicorp/go-multierror`
- `github.com/huandu/xstrings`
- `github.com/jmoiron/sqlx`
- `github.com/jmoiron/sqlx/reflectx`
- `github.com/josharian/intern`
- `github.com/json-iterator/go`
- `github.com/klauspost/compress`
- `github.com/klauspost/compress/fse`
- `github.com/klauspost/compress/huff0`
- `github.com/klauspost/compress/internal/le`
- `github.com/klauspost/compress/internal/snapref`
- `github.com/klauspost/compress/zstd`
- `github.com/klauspost/compress/zstd/internal/xxhash`
- `github.com/lann/builder`
- `github.com/lann/ps`
- ... (665 more)

**Stdlib deps**

- 225 packages

## `github.com/example/ktl/internal/workflows/buildsvc`

**Internal deps**

- `github.com/example/ktl/internal/capture`
- `github.com/example/ktl/internal/caststream`
- `github.com/example/ktl/internal/castutil`
- `github.com/example/ktl/internal/config`
- `github.com/example/ktl/internal/csvutil`
- `github.com/example/ktl/internal/deploy`
- `github.com/example/ktl/internal/dockerconfig`
- `github.com/example/ktl/internal/kube`
- `github.com/example/ktl/internal/logging`
- `github.com/example/ktl/internal/sqlitewriter`
- `github.com/example/ktl/internal/tailer`
- `github.com/example/ktl/pkg/buildkit`
- `github.com/example/ktl/pkg/compose`
- `github.com/example/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/asaskevich/govalidator`
- `github.com/blang/semver/v4`
- `github.com/chai2010/gettext-go`
- `github.com/chai2010/gettext-go/mo`
- `github.com/chai2010/gettext-go/plural`
- `github.com/chai2010/gettext-go/po`
- `github.com/compose-spec/compose-go/v2/consts`
- `github.com/compose-spec/compose-go/v2/dotenv`
- `github.com/compose-spec/compose-go/v2/errdefs`
- `github.com/compose-spec/compose-go/v2/format`
- `github.com/compose-spec/compose-go/v2/graph`
- `github.com/compose-spec/compose-go/v2/interpolation`
- `github.com/compose-spec/compose-go/v2/loader`
- `github.com/compose-spec/compose-go/v2/override`
- `github.com/compose-spec/compose-go/v2/paths`
- `github.com/compose-spec/compose-go/v2/schema`
- `github.com/compose-spec/compose-go/v2/template`
- `github.com/compose-spec/compose-go/v2/transform`
- `github.com/compose-spec/compose-go/v2/tree`
- `github.com/compose-spec/compose-go/v2/types`
- `github.com/compose-spec/compose-go/v2/utils`
- `github.com/compose-spec/compose-go/v2/validation`
- `github.com/containerd/console`
- `github.com/containerd/containerd/api/services/content/v1`
- `github.com/containerd/containerd/archive/compression`
- `github.com/containerd/containerd/content`
- `github.com/containerd/containerd/errdefs`
- `github.com/containerd/containerd/filters`
- `github.com/containerd/containerd/images`
- `github.com/containerd/containerd/labels`
- `github.com/containerd/containerd/pkg/randutil`
- `github.com/containerd/containerd/remotes`
- `github.com/containerd/containerd/v2/core/content`
- `github.com/containerd/containerd/v2/core/content/proxy`
- `github.com/containerd/containerd/v2/core/images`
- `github.com/containerd/containerd/v2/core/leases`
- `github.com/containerd/containerd/v2/core/remotes`
- `github.com/containerd/containerd/v2/core/remotes/docker`
- `github.com/containerd/containerd/v2/core/remotes/docker/auth`
- `github.com/containerd/containerd/v2/core/remotes/errors`
- `github.com/containerd/containerd/v2/core/transfer`
- `github.com/containerd/containerd/v2/defaults`
- `github.com/containerd/containerd/v2/internal/fsverity`
- `github.com/containerd/containerd/v2/internal/lazyregexp`
- `github.com/containerd/containerd/v2/internal/randutil`
- `github.com/containerd/containerd/v2/pkg/archive/compression`
- `github.com/containerd/containerd/v2/pkg/filters`
- `github.com/containerd/containerd/v2/pkg/identifiers`
- `github.com/containerd/containerd/v2/pkg/labels`
- `github.com/containerd/containerd/v2/pkg/namespaces`
- `github.com/containerd/containerd/v2/pkg/protobuf`
- `github.com/containerd/containerd/v2/pkg/protobuf/types`
- `github.com/containerd/containerd/v2/pkg/reference`
- `github.com/containerd/containerd/v2/pkg/tracing`
- `github.com/containerd/containerd/v2/plugins/content/local`
- `github.com/containerd/containerd/v2/plugins/services/content/contentserver`
- `github.com/containerd/containerd/v2/version`
- `github.com/containerd/continuity/sysx`
- `github.com/containerd/errdefs`
- `github.com/containerd/errdefs/pkg/errgrpc`
- `github.com/containerd/errdefs/pkg/internal/cause`
- `github.com/containerd/errdefs/pkg/internal/types`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/containerd/stargz-snapshotter/estargz`
- `github.com/containerd/stargz-snapshotter/estargz/errorutil`
- `github.com/containerd/ttrpc`
- `github.com/containerd/typeurl/v2`
- `github.com/cyphar/filepath-securejoin`
- `github.com/cyphar/filepath-securejoin/internal/consts`
- `github.com/davecgh/go-spew/spew`
- `github.com/distribution/reference`
- ... (1112 more)

**Stdlib deps**

- 233 packages

## `github.com/example/ktl/pkg/api/v1`

**Internal deps**

- (none)

**Third-party deps**

- `golang.org/x/net/http/httpguts`
- `golang.org/x/net/http2`
- `golang.org/x/net/http2/hpack`
- `golang.org/x/net/idna`
- `golang.org/x/net/internal/httpcommon`
- `golang.org/x/net/internal/timeseries`
- `golang.org/x/net/trace`
- `golang.org/x/sys/unix`
- `golang.org/x/text/secure/bidirule`
- `golang.org/x/text/transform`
- `golang.org/x/text/unicode/bidi`
- `golang.org/x/text/unicode/norm`
- `google.golang.org/genproto/googleapis/rpc/status`
- `google.golang.org/grpc`
- `google.golang.org/grpc/attributes`
- `google.golang.org/grpc/backoff`
- `google.golang.org/grpc/balancer`
- `google.golang.org/grpc/balancer/base`
- `google.golang.org/grpc/balancer/endpointsharding`
- `google.golang.org/grpc/balancer/grpclb/state`
- `google.golang.org/grpc/balancer/pickfirst`
- `google.golang.org/grpc/balancer/pickfirst/internal`
- `google.golang.org/grpc/balancer/pickfirst/pickfirstleaf`
- `google.golang.org/grpc/balancer/roundrobin`
- `google.golang.org/grpc/binarylog/grpc_binarylog_v1`
- `google.golang.org/grpc/channelz`
- `google.golang.org/grpc/codes`
- `google.golang.org/grpc/connectivity`
- `google.golang.org/grpc/credentials`
- `google.golang.org/grpc/credentials/insecure`
- `google.golang.org/grpc/encoding`
- `google.golang.org/grpc/encoding/proto`
- `google.golang.org/grpc/experimental/stats`
- `google.golang.org/grpc/grpclog`
- `google.golang.org/grpc/grpclog/internal`
- `google.golang.org/grpc/internal`
- `google.golang.org/grpc/internal/backoff`
- `google.golang.org/grpc/internal/balancer/gracefulswitch`
- `google.golang.org/grpc/internal/balancerload`
- `google.golang.org/grpc/internal/binarylog`
- `google.golang.org/grpc/internal/buffer`
- `google.golang.org/grpc/internal/channelz`
- `google.golang.org/grpc/internal/credentials`
- `google.golang.org/grpc/internal/envconfig`
- `google.golang.org/grpc/internal/grpclog`
- `google.golang.org/grpc/internal/grpcsync`
- `google.golang.org/grpc/internal/grpcutil`
- `google.golang.org/grpc/internal/idle`
- `google.golang.org/grpc/internal/metadata`
- `google.golang.org/grpc/internal/pretty`
- `google.golang.org/grpc/internal/proxyattributes`
- `google.golang.org/grpc/internal/resolver`
- `google.golang.org/grpc/internal/resolver/delegatingresolver`
- `google.golang.org/grpc/internal/resolver/dns`
- `google.golang.org/grpc/internal/resolver/dns/internal`
- `google.golang.org/grpc/internal/resolver/passthrough`
- `google.golang.org/grpc/internal/resolver/unix`
- `google.golang.org/grpc/internal/serviceconfig`
- `google.golang.org/grpc/internal/stats`
- `google.golang.org/grpc/internal/status`
- `google.golang.org/grpc/internal/syscall`
- `google.golang.org/grpc/internal/transport`
- `google.golang.org/grpc/internal/transport/networktype`
- `google.golang.org/grpc/keepalive`
- `google.golang.org/grpc/mem`
- `google.golang.org/grpc/metadata`
- `google.golang.org/grpc/peer`
- `google.golang.org/grpc/resolver`
- `google.golang.org/grpc/resolver/dns`
- `google.golang.org/grpc/serviceconfig`
- `google.golang.org/grpc/stats`
- `google.golang.org/grpc/status`
- `google.golang.org/grpc/tap`
- `google.golang.org/protobuf/encoding/protojson`
- `google.golang.org/protobuf/encoding/prototext`
- `google.golang.org/protobuf/encoding/protowire`
- `google.golang.org/protobuf/internal/descfmt`
- `google.golang.org/protobuf/internal/descopts`
- `google.golang.org/protobuf/internal/detrand`
- `google.golang.org/protobuf/internal/editiondefaults`
- ... (26 more)

**Stdlib deps**

- 191 packages

## `github.com/example/ktl/pkg/buildkit`

**Internal deps**

- (none)

**Third-party deps**

- `github.com/containerd/console`
- `github.com/containerd/containerd/api/services/content/v1`
- `github.com/containerd/containerd/v2/core/content`
- `github.com/containerd/containerd/v2/core/content/proxy`
- `github.com/containerd/containerd/v2/core/images`
- `github.com/containerd/containerd/v2/core/leases`
- `github.com/containerd/containerd/v2/core/remotes`
- `github.com/containerd/containerd/v2/core/remotes/docker`
- `github.com/containerd/containerd/v2/core/remotes/docker/auth`
- `github.com/containerd/containerd/v2/core/remotes/errors`
- `github.com/containerd/containerd/v2/core/transfer`
- `github.com/containerd/containerd/v2/defaults`
- `github.com/containerd/containerd/v2/internal/fsverity`
- `github.com/containerd/containerd/v2/internal/lazyregexp`
- `github.com/containerd/containerd/v2/internal/randutil`
- `github.com/containerd/containerd/v2/pkg/archive/compression`
- `github.com/containerd/containerd/v2/pkg/filters`
- `github.com/containerd/containerd/v2/pkg/identifiers`
- `github.com/containerd/containerd/v2/pkg/labels`
- `github.com/containerd/containerd/v2/pkg/namespaces`
- `github.com/containerd/containerd/v2/pkg/protobuf`
- `github.com/containerd/containerd/v2/pkg/protobuf/types`
- `github.com/containerd/containerd/v2/pkg/reference`
- `github.com/containerd/containerd/v2/pkg/tracing`
- `github.com/containerd/containerd/v2/plugins/content/local`
- `github.com/containerd/containerd/v2/plugins/services/content/contentserver`
- `github.com/containerd/containerd/v2/version`
- `github.com/containerd/continuity/sysx`
- `github.com/containerd/errdefs`
- `github.com/containerd/errdefs/pkg/errgrpc`
- `github.com/containerd/errdefs/pkg/internal/cause`
- `github.com/containerd/errdefs/pkg/internal/types`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/containerd/ttrpc`
- `github.com/containerd/typeurl/v2`
- `github.com/distribution/reference`
- `github.com/docker/cli/cli/config`
- `github.com/docker/cli/cli/config/configfile`
- `github.com/docker/cli/cli/config/credentials`
- `github.com/docker/cli/cli/config/memorystore`
- `github.com/docker/cli/cli/config/types`
- `github.com/docker/cli/cli/connhelper/commandconn`
- `github.com/docker/docker-credential-helpers/client`
- `github.com/docker/docker-credential-helpers/credentials`
- `github.com/felixge/httpsnoop`
- `github.com/go-logr/logr`
- `github.com/go-logr/logr/funcr`
- `github.com/go-logr/stdr`
- `github.com/gofrs/flock`
- `github.com/gogo/protobuf/proto`
- `github.com/golang/protobuf/jsonpb`
- `github.com/golang/protobuf/proto`
- `github.com/golang/protobuf/ptypes/any`
- `github.com/golang/protobuf/ptypes/timestamp`
- `github.com/google/go-cmp/cmp`
- `github.com/google/go-cmp/cmp/internal/diff`
- `github.com/google/go-cmp/cmp/internal/flags`
- `github.com/google/go-cmp/cmp/internal/function`
- `github.com/google/go-cmp/cmp/internal/value`
- `github.com/google/shlex`
- `github.com/google/uuid`
- `github.com/grpc-ecosystem/grpc-gateway/v2/internal/httprule`
- `github.com/grpc-ecosystem/grpc-gateway/v2/runtime`
- `github.com/grpc-ecosystem/grpc-gateway/v2/utilities`
- `github.com/hashicorp/go-cleanhttp`
- `github.com/in-toto/in-toto-golang/in_toto`
- `github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/common`
- `github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.1`
- `github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.2`
- `github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1`
- `github.com/klauspost/compress`
- `github.com/klauspost/compress/fse`
- `github.com/klauspost/compress/huff0`
- `github.com/klauspost/compress/internal/le`
- `github.com/klauspost/compress/internal/snapref`
- `github.com/klauspost/compress/zstd`
- `github.com/klauspost/compress/zstd/internal/xxhash`
- `github.com/moby/buildkit/api/services/control`
- `github.com/moby/buildkit/api/types`
- ... (244 more)

**Stdlib deps**

- 210 packages

## `github.com/example/ktl/pkg/compose`

**Internal deps**

- `github.com/example/ktl/internal/csvutil`
- `github.com/example/ktl/pkg/buildkit`
- `github.com/example/ktl/pkg/registry`

**Third-party deps**

- `github.com/compose-spec/compose-go/v2/consts`
- `github.com/compose-spec/compose-go/v2/dotenv`
- `github.com/compose-spec/compose-go/v2/errdefs`
- `github.com/compose-spec/compose-go/v2/format`
- `github.com/compose-spec/compose-go/v2/graph`
- `github.com/compose-spec/compose-go/v2/interpolation`
- `github.com/compose-spec/compose-go/v2/loader`
- `github.com/compose-spec/compose-go/v2/override`
- `github.com/compose-spec/compose-go/v2/paths`
- `github.com/compose-spec/compose-go/v2/schema`
- `github.com/compose-spec/compose-go/v2/template`
- `github.com/compose-spec/compose-go/v2/transform`
- `github.com/compose-spec/compose-go/v2/tree`
- `github.com/compose-spec/compose-go/v2/types`
- `github.com/compose-spec/compose-go/v2/utils`
- `github.com/compose-spec/compose-go/v2/validation`
- `github.com/containerd/console`
- `github.com/containerd/containerd/api/services/content/v1`
- `github.com/containerd/containerd/v2/core/content`
- `github.com/containerd/containerd/v2/core/content/proxy`
- `github.com/containerd/containerd/v2/core/images`
- `github.com/containerd/containerd/v2/core/leases`
- `github.com/containerd/containerd/v2/core/remotes`
- `github.com/containerd/containerd/v2/core/remotes/docker`
- `github.com/containerd/containerd/v2/core/remotes/docker/auth`
- `github.com/containerd/containerd/v2/core/remotes/errors`
- `github.com/containerd/containerd/v2/core/transfer`
- `github.com/containerd/containerd/v2/defaults`
- `github.com/containerd/containerd/v2/internal/fsverity`
- `github.com/containerd/containerd/v2/internal/lazyregexp`
- `github.com/containerd/containerd/v2/internal/randutil`
- `github.com/containerd/containerd/v2/pkg/archive/compression`
- `github.com/containerd/containerd/v2/pkg/filters`
- `github.com/containerd/containerd/v2/pkg/identifiers`
- `github.com/containerd/containerd/v2/pkg/labels`
- `github.com/containerd/containerd/v2/pkg/namespaces`
- `github.com/containerd/containerd/v2/pkg/protobuf`
- `github.com/containerd/containerd/v2/pkg/protobuf/types`
- `github.com/containerd/containerd/v2/pkg/reference`
- `github.com/containerd/containerd/v2/pkg/tracing`
- `github.com/containerd/containerd/v2/plugins/content/local`
- `github.com/containerd/containerd/v2/plugins/services/content/contentserver`
- `github.com/containerd/containerd/v2/version`
- `github.com/containerd/continuity/sysx`
- `github.com/containerd/errdefs`
- `github.com/containerd/errdefs/pkg/errgrpc`
- `github.com/containerd/errdefs/pkg/internal/cause`
- `github.com/containerd/errdefs/pkg/internal/types`
- `github.com/containerd/log`
- `github.com/containerd/platforms`
- `github.com/containerd/stargz-snapshotter/estargz`
- `github.com/containerd/stargz-snapshotter/estargz/errorutil`
- `github.com/containerd/ttrpc`
- `github.com/containerd/typeurl/v2`
- `github.com/distribution/reference`
- `github.com/docker/cli/cli/config`
- `github.com/docker/cli/cli/config/configfile`
- `github.com/docker/cli/cli/config/credentials`
- `github.com/docker/cli/cli/config/memorystore`
- `github.com/docker/cli/cli/config/types`
- `github.com/docker/cli/cli/connhelper/commandconn`
- `github.com/docker/distribution/registry/client/auth/challenge`
- `github.com/docker/docker-credential-helpers/client`
- `github.com/docker/docker-credential-helpers/credentials`
- `github.com/docker/go-connections/nat`
- `github.com/docker/go-units`
- `github.com/felixge/httpsnoop`
- `github.com/go-logr/logr`
- `github.com/go-logr/logr/funcr`
- `github.com/go-logr/stdr`
- `github.com/go-viper/mapstructure/v2`
- `github.com/go-viper/mapstructure/v2/internal/errors`
- `github.com/gofrs/flock`
- `github.com/gogo/protobuf/proto`
- `github.com/golang/protobuf/jsonpb`
- `github.com/golang/protobuf/proto`
- `github.com/golang/protobuf/ptypes/any`
- `github.com/golang/protobuf/ptypes/timestamp`
- `github.com/google/go-cmp/cmp`
- `github.com/google/go-cmp/cmp/internal/diff`
- ... (305 more)

**Stdlib deps**

- 213 packages

## `github.com/example/ktl/pkg/registry`

**Internal deps**

- (none)

**Third-party deps**

- `github.com/containerd/stargz-snapshotter/estargz`
- `github.com/containerd/stargz-snapshotter/estargz/errorutil`
- `github.com/docker/cli/cli/config`
- `github.com/docker/cli/cli/config/configfile`
- `github.com/docker/cli/cli/config/credentials`
- `github.com/docker/cli/cli/config/memorystore`
- `github.com/docker/cli/cli/config/types`
- `github.com/docker/distribution/registry/client/auth/challenge`
- `github.com/docker/docker-credential-helpers/client`
- `github.com/docker/docker-credential-helpers/credentials`
- `github.com/google/go-containerregistry/internal/and`
- `github.com/google/go-containerregistry/internal/compression`
- `github.com/google/go-containerregistry/internal/estargz`
- `github.com/google/go-containerregistry/internal/gzip`
- `github.com/google/go-containerregistry/internal/redact`
- `github.com/google/go-containerregistry/internal/retry`
- `github.com/google/go-containerregistry/internal/retry/wait`
- `github.com/google/go-containerregistry/internal/verify`
- `github.com/google/go-containerregistry/internal/windows`
- `github.com/google/go-containerregistry/internal/zstd`
- `github.com/google/go-containerregistry/pkg/authn`
- `github.com/google/go-containerregistry/pkg/compression`
- `github.com/google/go-containerregistry/pkg/crane`
- `github.com/google/go-containerregistry/pkg/legacy`
- `github.com/google/go-containerregistry/pkg/legacy/tarball`
- `github.com/google/go-containerregistry/pkg/logs`
- `github.com/google/go-containerregistry/pkg/name`
- `github.com/google/go-containerregistry/pkg/v1`
- `github.com/google/go-containerregistry/pkg/v1/empty`
- `github.com/google/go-containerregistry/pkg/v1/layout`
- `github.com/google/go-containerregistry/pkg/v1/match`
- `github.com/google/go-containerregistry/pkg/v1/mutate`
- `github.com/google/go-containerregistry/pkg/v1/partial`
- `github.com/google/go-containerregistry/pkg/v1/remote`
- `github.com/google/go-containerregistry/pkg/v1/remote/transport`
- `github.com/google/go-containerregistry/pkg/v1/stream`
- `github.com/google/go-containerregistry/pkg/v1/tarball`
- `github.com/google/go-containerregistry/pkg/v1/types`
- `github.com/klauspost/compress`
- `github.com/klauspost/compress/fse`
- `github.com/klauspost/compress/huff0`
- `github.com/klauspost/compress/internal/le`
- `github.com/klauspost/compress/internal/snapref`
- `github.com/klauspost/compress/zstd`
- `github.com/klauspost/compress/zstd/internal/xxhash`
- `github.com/mitchellh/go-homedir`
- `github.com/opencontainers/go-digest`
- `github.com/opencontainers/image-spec/specs-go`
- `github.com/opencontainers/image-spec/specs-go/v1`
- `github.com/pkg/errors`
- `github.com/sirupsen/logrus`
- `github.com/vbatts/tar-split/archive/tar`
- `golang.org/x/sync/errgroup`
- `golang.org/x/sys/unix`

**Stdlib deps**

- 191 packages

