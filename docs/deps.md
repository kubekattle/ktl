# Dependency Map

This file tracks the dependency graph of the project.

## `github.com/kubekattle/ktl/cmd/capture`

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

- 190 packages

## `github.com/kubekattle/ktl/cmd/ktl`

**Internal deps**

- `github.com/kubekattle/ktl/docs`
- `github.com/kubekattle/ktl/internal/api/convert`
- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/capture`
- `github.com/kubekattle/ktl/internal/caststream`
- `github.com/kubekattle/ktl/internal/castutil`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/csvutil`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/dockerconfig`
- `github.com/kubekattle/ktl/internal/envcatalog`
- `github.com/kubekattle/ktl/internal/featureflags`
- `github.com/kubekattle/ktl/internal/grpcutil`
- `github.com/kubekattle/ktl/internal/helpui`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/logging`
- `github.com/kubekattle/ktl/internal/mirrorbus`
- `github.com/kubekattle/ktl/internal/policy`
- `github.com/kubekattle/ktl/internal/secrets`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/stack`
- `github.com/kubekattle/ktl/internal/tailer`
- `github.com/kubekattle/ktl/internal/telemetry`
- `github.com/kubekattle/ktl/internal/ui`
- `github.com/kubekattle/ktl/internal/verify`
- `github.com/kubekattle/ktl/internal/version`
- `github.com/kubekattle/ktl/internal/workflows/buildsvc`
- `github.com/kubekattle/ktl/pkg/api/ktl/api/v1`
- `github.com/kubekattle/ktl/pkg/buildkit`
- `github.com/kubekattle/ktl/pkg/compose`
- `github.com/kubekattle/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/OneOfOne/xxhash`
- `github.com/agnivade/levenshtein`
- `github.com/asaskevich/govalidator`
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- ... (1214 more)

**Stdlib deps**

- 239 packages

## `github.com/kubekattle/ktl/cmd/ktl-agent`

**Internal deps**

- `github.com/kubekattle/ktl/internal/agent`
- `github.com/kubekattle/ktl/internal/api/convert`
- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/capture`
- `github.com/kubekattle/ktl/internal/caststream`
- `github.com/kubekattle/ktl/internal/castutil`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/csvutil`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/dockerconfig`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/logging`
- `github.com/kubekattle/ktl/internal/policy`
- `github.com/kubekattle/ktl/internal/secrets`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`
- `github.com/kubekattle/ktl/internal/telemetry`
- `github.com/kubekattle/ktl/internal/ui`
- `github.com/kubekattle/ktl/internal/verify`
- `github.com/kubekattle/ktl/internal/version`
- `github.com/kubekattle/ktl/internal/workflows/buildsvc`
- `github.com/kubekattle/ktl/pkg/api/ktl/api/v1`
- `github.com/kubekattle/ktl/pkg/buildkit`
- `github.com/kubekattle/ktl/pkg/compose`
- `github.com/kubekattle/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/OneOfOne/xxhash`
- `github.com/agnivade/levenshtein`
- `github.com/asaskevich/govalidator`
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- ... (1195 more)

**Stdlib deps**

- 239 packages

## `github.com/kubekattle/ktl/cmd/package`

**Internal deps**

- `github.com/kubekattle/ktl/internal/chartarchive`
- `github.com/kubekattle/ktl/internal/version`

**Third-party deps**

- `github.com/Masterminds/semver/v3`
- `github.com/dustin/go-humanize`
- `github.com/google/uuid`
- `github.com/mattn/go-isatty`
- `github.com/ncruces/go-strftime`
- `github.com/pkg/errors`
- `github.com/remyoudompheng/bigfft`
- `github.com/spf13/cobra`
- `github.com/spf13/pflag`
- `go.yaml.in/yaml/v2`
- `golang.org/x/exp/constraints`
- `golang.org/x/sys/unix`
- `helm.sh/helm/v3/internal/sympath`
- `helm.sh/helm/v3/pkg/chart`
- `helm.sh/helm/v3/pkg/chart/loader`
- `helm.sh/helm/v3/pkg/ignore`
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
- `sigs.k8s.io/yaml`

**Stdlib deps**

- 194 packages

## `github.com/kubekattle/ktl/cmd/verify`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/policy`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`
- `github.com/kubekattle/ktl/internal/telemetry`
- `github.com/kubekattle/ktl/internal/ui`
- `github.com/kubekattle/ktl/internal/verify`
- `github.com/kubekattle/ktl/internal/verify/config`
- `github.com/kubekattle/ktl/internal/verify/engine`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/OneOfOne/xxhash`
- `github.com/agnivade/levenshtein`
- `github.com/asaskevich/govalidator`
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- ... (865 more)

**Stdlib deps**

- 234 packages

## `github.com/kubekattle/ktl/docs`

**Internal deps**

- (none)

**Third-party deps**

- (none)

**Stdlib deps**

- 47 packages

## `github.com/kubekattle/ktl/internal/agent`

**Internal deps**

- `github.com/kubekattle/ktl/internal/api/convert`
- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/capture`
- `github.com/kubekattle/ktl/internal/caststream`
- `github.com/kubekattle/ktl/internal/castutil`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/csvutil`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/dockerconfig`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/logging`
- `github.com/kubekattle/ktl/internal/policy`
- `github.com/kubekattle/ktl/internal/secrets`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`
- `github.com/kubekattle/ktl/internal/telemetry`
- `github.com/kubekattle/ktl/internal/ui`
- `github.com/kubekattle/ktl/internal/verify`
- `github.com/kubekattle/ktl/internal/version`
- `github.com/kubekattle/ktl/internal/workflows/buildsvc`
- `github.com/kubekattle/ktl/pkg/api/ktl/api/v1`
- `github.com/kubekattle/ktl/pkg/buildkit`
- `github.com/kubekattle/ktl/pkg/compose`
- `github.com/kubekattle/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/OneOfOne/xxhash`
- `github.com/agnivade/levenshtein`
- `github.com/asaskevich/govalidator`
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- ... (1195 more)

**Stdlib deps**

- 239 packages

## `github.com/kubekattle/ktl/internal/api/convert`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/capture`
- `github.com/kubekattle/ktl/internal/caststream`
- `github.com/kubekattle/ktl/internal/castutil`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/csvutil`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/dockerconfig`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/logging`
- `github.com/kubekattle/ktl/internal/policy`
- `github.com/kubekattle/ktl/internal/secrets`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`
- `github.com/kubekattle/ktl/internal/telemetry`
- `github.com/kubekattle/ktl/internal/ui`
- `github.com/kubekattle/ktl/internal/workflows/buildsvc`
- `github.com/kubekattle/ktl/pkg/api/ktl/api/v1`
- `github.com/kubekattle/ktl/pkg/buildkit`
- `github.com/kubekattle/ktl/pkg/compose`
- `github.com/kubekattle/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/OneOfOne/xxhash`
- `github.com/agnivade/levenshtein`
- `github.com/asaskevich/govalidator`
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- ... (1191 more)

**Stdlib deps**

- 239 packages

## `github.com/kubekattle/ktl/internal/appconfig`

**Internal deps**

- (none)

**Third-party deps**

- `gopkg.in/yaml.v3`

**Stdlib deps**

- 67 packages

## `github.com/kubekattle/ktl/internal/capture`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`

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
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- `github.com/aws/smithy-go/ptr`
- `github.com/aws/smithy-go/rand`
- ... (763 more)

**Stdlib deps**

- 227 packages

## `github.com/kubekattle/ktl/internal/caststream`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`

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
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- `github.com/aws/smithy-go/ptr`
- `github.com/aws/smithy-go/rand`
- ... (730 more)

**Stdlib deps**

- 227 packages

## `github.com/kubekattle/ktl/internal/castutil`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/caststream`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`

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
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- `github.com/aws/smithy-go/ptr`
- `github.com/aws/smithy-go/rand`
- ... (730 more)

**Stdlib deps**

- 227 packages

## `github.com/kubekattle/ktl/internal/chartarchive`

**Internal deps**

- (none)

**Third-party deps**

- `github.com/Masterminds/semver/v3`
- `github.com/dustin/go-humanize`
- `github.com/google/uuid`
- `github.com/mattn/go-isatty`
- `github.com/ncruces/go-strftime`
- `github.com/pkg/errors`
- `github.com/remyoudompheng/bigfft`
- `go.yaml.in/yaml/v2`
- `golang.org/x/exp/constraints`
- `golang.org/x/sys/unix`
- `helm.sh/helm/v3/internal/sympath`
- `helm.sh/helm/v3/pkg/chart`
- `helm.sh/helm/v3/pkg/chart/loader`
- `helm.sh/helm/v3/pkg/ignore`
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
- `sigs.k8s.io/yaml`

**Stdlib deps**

- 190 packages

## `github.com/kubekattle/ktl/internal/config`

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

## `github.com/kubekattle/ktl/internal/csvutil`

**Internal deps**

- (none)

**Third-party deps**

- (none)

**Stdlib deps**

- 62 packages

## `github.com/kubekattle/ktl/internal/deploy`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`

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
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- `github.com/aws/smithy-go/ptr`
- `github.com/aws/smithy-go/rand`
- ... (730 more)

**Stdlib deps**

- 227 packages

## `github.com/kubekattle/ktl/internal/dockerconfig`

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

## `github.com/kubekattle/ktl/internal/featureflags`

**Internal deps**

- (none)

**Third-party deps**

- (none)

**Stdlib deps**

- 61 packages

## `github.com/kubekattle/ktl/internal/grpcutil`

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

## `github.com/kubekattle/ktl/internal/helpui`

**Internal deps**

- `github.com/kubekattle/ktl/docs`
- `github.com/kubekattle/ktl/internal/envcatalog`
- `github.com/kubekattle/ktl/internal/featureflags`
- `github.com/kubekattle/ktl/internal/version`

**Third-party deps**

- `github.com/go-logr/logr`
- `github.com/spf13/cobra`
- `github.com/spf13/pflag`

**Stdlib deps**

- 192 packages

## `github.com/kubekattle/ktl/internal/kube`

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

## `github.com/kubekattle/ktl/internal/logging`

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

## `github.com/kubekattle/ktl/internal/mirrorbus`

**Internal deps**

- `github.com/kubekattle/ktl/internal/api/convert`
- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/capture`
- `github.com/kubekattle/ktl/internal/caststream`
- `github.com/kubekattle/ktl/internal/castutil`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/csvutil`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/dockerconfig`
- `github.com/kubekattle/ktl/internal/grpcutil`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/logging`
- `github.com/kubekattle/ktl/internal/policy`
- `github.com/kubekattle/ktl/internal/secrets`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`
- `github.com/kubekattle/ktl/internal/telemetry`
- `github.com/kubekattle/ktl/internal/ui`
- `github.com/kubekattle/ktl/internal/workflows/buildsvc`
- `github.com/kubekattle/ktl/pkg/api/ktl/api/v1`
- `github.com/kubekattle/ktl/pkg/buildkit`
- `github.com/kubekattle/ktl/pkg/compose`
- `github.com/kubekattle/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/OneOfOne/xxhash`
- `github.com/agnivade/levenshtein`
- `github.com/asaskevich/govalidator`
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- ... (1191 more)

**Stdlib deps**

- 239 packages

## `github.com/kubekattle/ktl/internal/policy`

**Internal deps**

- (none)

**Third-party deps**

- `github.com/OneOfOne/xxhash`
- `github.com/agnivade/levenshtein`
- `github.com/beorn7/perks/quantile`
- `github.com/cespare/xxhash/v2`
- `github.com/go-ini/ini`
- `github.com/go-logr/logr`
- `github.com/go-logr/logr/funcr`
- `github.com/go-logr/stdr`
- `github.com/gobwas/glob`
- `github.com/gobwas/glob/compiler`
- `github.com/gobwas/glob/match`
- `github.com/gobwas/glob/syntax`
- `github.com/gobwas/glob/syntax/ast`
- `github.com/gobwas/glob/syntax/lexer`
- `github.com/gobwas/glob/util/runes`
- `github.com/gobwas/glob/util/strings`
- `github.com/google/uuid`
- `github.com/gorilla/mux`
- `github.com/munnerz/goautoneg`
- `github.com/open-policy-agent/opa/ast`
- `github.com/open-policy-agent/opa/ast/internal/scanner`
- `github.com/open-policy-agent/opa/ast/internal/tokens`
- `github.com/open-policy-agent/opa/ast/json`
- `github.com/open-policy-agent/opa/ast/location`
- `github.com/open-policy-agent/opa/bundle`
- `github.com/open-policy-agent/opa/capabilities`
- `github.com/open-policy-agent/opa/config`
- `github.com/open-policy-agent/opa/format`
- `github.com/open-policy-agent/opa/hooks`
- `github.com/open-policy-agent/opa/internal/bundle`
- `github.com/open-policy-agent/opa/internal/cidr/merge`
- `github.com/open-policy-agent/opa/internal/compiler`
- `github.com/open-policy-agent/opa/internal/compiler/wasm`
- `github.com/open-policy-agent/opa/internal/compiler/wasm/opa`
- `github.com/open-policy-agent/opa/internal/config`
- `github.com/open-policy-agent/opa/internal/debug`
- `github.com/open-policy-agent/opa/internal/deepcopy`
- `github.com/open-policy-agent/opa/internal/edittree`
- `github.com/open-policy-agent/opa/internal/edittree/bitvector`
- `github.com/open-policy-agent/opa/internal/file/archive`
- `github.com/open-policy-agent/opa/internal/file/url`
- `github.com/open-policy-agent/opa/internal/future`
- `github.com/open-policy-agent/opa/internal/gojsonschema`
- `github.com/open-policy-agent/opa/internal/gqlparser/ast`
- `github.com/open-policy-agent/opa/internal/gqlparser/gqlerror`
- `github.com/open-policy-agent/opa/internal/gqlparser/lexer`
- `github.com/open-policy-agent/opa/internal/gqlparser/parser`
- `github.com/open-policy-agent/opa/internal/gqlparser/validator`
- `github.com/open-policy-agent/opa/internal/gqlparser/validator/rules`
- `github.com/open-policy-agent/opa/internal/json/patch`
- `github.com/open-policy-agent/opa/internal/jwx/buffer`
- `github.com/open-policy-agent/opa/internal/jwx/jwa`
- `github.com/open-policy-agent/opa/internal/jwx/jwk`
- `github.com/open-policy-agent/opa/internal/jwx/jws`
- `github.com/open-policy-agent/opa/internal/jwx/jws/sign`
- `github.com/open-policy-agent/opa/internal/jwx/jws/verify`
- `github.com/open-policy-agent/opa/internal/lcss`
- `github.com/open-policy-agent/opa/internal/leb128`
- `github.com/open-policy-agent/opa/internal/merge`
- `github.com/open-policy-agent/opa/internal/planner`
- `github.com/open-policy-agent/opa/internal/providers/aws`
- `github.com/open-policy-agent/opa/internal/providers/aws/crypto`
- `github.com/open-policy-agent/opa/internal/providers/aws/v4`
- `github.com/open-policy-agent/opa/internal/ref`
- `github.com/open-policy-agent/opa/internal/rego/opa`
- `github.com/open-policy-agent/opa/internal/report`
- `github.com/open-policy-agent/opa/internal/runtime/init`
- `github.com/open-policy-agent/opa/internal/semver`
- `github.com/open-policy-agent/opa/internal/strings`
- `github.com/open-policy-agent/opa/internal/strvals`
- `github.com/open-policy-agent/opa/internal/uuid`
- `github.com/open-policy-agent/opa/internal/version`
- `github.com/open-policy-agent/opa/internal/wasm/constant`
- `github.com/open-policy-agent/opa/internal/wasm/encoding`
- `github.com/open-policy-agent/opa/internal/wasm/instruction`
- `github.com/open-policy-agent/opa/internal/wasm/module`
- `github.com/open-policy-agent/opa/internal/wasm/opcode`
- `github.com/open-policy-agent/opa/internal/wasm/sdk/opa/capabilities`
- `github.com/open-policy-agent/opa/internal/wasm/types`
- `github.com/open-policy-agent/opa/internal/wasm/util`
- ... (97 more)

**Stdlib deps**

- 208 packages

## `github.com/kubekattle/ktl/internal/secrets`

**Internal deps**

- (none)

**Third-party deps**

- `github.com/google/go-containerregistry/pkg/v1/types`
- `gopkg.in/yaml.v3`

**Stdlib deps**

- 184 packages

## `github.com/kubekattle/ktl/internal/secretstore`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`

**Third-party deps**

- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- `github.com/aws/smithy-go/ptr`
- `github.com/aws/smithy-go/rand`
- `github.com/aws/smithy-go/time`
- `github.com/aws/smithy-go/tracing`
- `github.com/aws/smithy-go/transport/http`
- `github.com/aws/smithy-go/transport/http/internal/io`
- `github.com/cenkalti/backoff/v4`
- `github.com/go-jose/go-jose/v4`
- `github.com/go-jose/go-jose/v4/cipher`
- `github.com/go-jose/go-jose/v4/json`
- `github.com/go-jose/go-jose/v4/jwt`
- ... (34 more)

**Stdlib deps**

- 192 packages

## `github.com/kubekattle/ktl/internal/stack`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`
- `github.com/kubekattle/ktl/internal/ui`
- `github.com/kubekattle/ktl/internal/version`

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
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- `github.com/aws/smithy-go/ptr`
- `github.com/aws/smithy-go/rand`
- ... (763 more)

**Stdlib deps**

- 227 packages

## `github.com/kubekattle/ktl/internal/tailer`

**Internal deps**

- `github.com/kubekattle/ktl/internal/config`

**Third-party deps**

- `github.com/davecgh/go-spew/spew`
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
- `github.com/pkg/errors`
- `github.com/pmezard/go-difflib/difflib`
- `github.com/spf13/cobra`
- `github.com/spf13/pflag`
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
- `google.golang.org/protobuf/runtime/protoimpl`
- `google.golang.org/protobuf/types/descriptorpb`
- `google.golang.org/protobuf/types/known/anypb`
- `gopkg.in/evanphx/json-patch.v4`
- ... (283 more)

**Stdlib deps**

- 210 packages

## `github.com/kubekattle/ktl/internal/telemetry`

**Internal deps**

- (none)

**Third-party deps**

- (none)

**Stdlib deps**

- 60 packages

## `github.com/kubekattle/ktl/internal/ui`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`

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
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- `github.com/aws/smithy-go/ptr`
- `github.com/aws/smithy-go/rand`
- ... (730 more)

**Stdlib deps**

- 227 packages

## `github.com/kubekattle/ktl/internal/verify`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/policy`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`
- `github.com/kubekattle/ktl/internal/ui`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/OneOfOne/xxhash`
- `github.com/agnivade/levenshtein`
- `github.com/asaskevich/govalidator`
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- ... (865 more)

**Stdlib deps**

- 234 packages

## `github.com/kubekattle/ktl/internal/verify/config`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/policy`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`
- `github.com/kubekattle/ktl/internal/ui`
- `github.com/kubekattle/ktl/internal/verify`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/OneOfOne/xxhash`
- `github.com/agnivade/levenshtein`
- `github.com/asaskevich/govalidator`
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- ... (865 more)

**Stdlib deps**

- 234 packages

## `github.com/kubekattle/ktl/internal/verify/engine`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/policy`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`
- `github.com/kubekattle/ktl/internal/telemetry`
- `github.com/kubekattle/ktl/internal/ui`
- `github.com/kubekattle/ktl/internal/verify`
- `github.com/kubekattle/ktl/internal/verify/config`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/OneOfOne/xxhash`
- `github.com/agnivade/levenshtein`
- `github.com/asaskevich/govalidator`
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- ... (865 more)

**Stdlib deps**

- 234 packages

## `github.com/kubekattle/ktl/internal/version`

**Internal deps**

- (none)

**Third-party deps**

- (none)

**Stdlib deps**

- 58 packages

## `github.com/kubekattle/ktl/internal/workflows/buildsvc`

**Internal deps**

- `github.com/kubekattle/ktl/internal/appconfig`
- `github.com/kubekattle/ktl/internal/capture`
- `github.com/kubekattle/ktl/internal/caststream`
- `github.com/kubekattle/ktl/internal/castutil`
- `github.com/kubekattle/ktl/internal/config`
- `github.com/kubekattle/ktl/internal/csvutil`
- `github.com/kubekattle/ktl/internal/deploy`
- `github.com/kubekattle/ktl/internal/dockerconfig`
- `github.com/kubekattle/ktl/internal/kube`
- `github.com/kubekattle/ktl/internal/logging`
- `github.com/kubekattle/ktl/internal/policy`
- `github.com/kubekattle/ktl/internal/secrets`
- `github.com/kubekattle/ktl/internal/secretstore`
- `github.com/kubekattle/ktl/internal/tailer`
- `github.com/kubekattle/ktl/internal/telemetry`
- `github.com/kubekattle/ktl/internal/ui`
- `github.com/kubekattle/ktl/pkg/buildkit`
- `github.com/kubekattle/ktl/pkg/compose`
- `github.com/kubekattle/ktl/pkg/registry`

**Third-party deps**

- `dario.cat/mergo`
- `github.com/BurntSushi/toml`
- `github.com/BurntSushi/toml/internal`
- `github.com/MakeNowJust/heredoc`
- `github.com/Masterminds/goutils`
- `github.com/Masterminds/semver/v3`
- `github.com/Masterminds/sprig/v3`
- `github.com/Masterminds/squirrel`
- `github.com/OneOfOne/xxhash`
- `github.com/agnivade/levenshtein`
- `github.com/asaskevich/govalidator`
- `github.com/aws/aws-sdk-go-v2/aws`
- `github.com/aws/aws-sdk-go-v2/aws/defaults`
- `github.com/aws/aws-sdk-go-v2/aws/middleware`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/query`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/restjson`
- `github.com/aws/aws-sdk-go-v2/aws/protocol/xml`
- `github.com/aws/aws-sdk-go-v2/aws/ratelimit`
- `github.com/aws/aws-sdk-go-v2/aws/retry`
- `github.com/aws/aws-sdk-go-v2/aws/signer/internal/v4`
- `github.com/aws/aws-sdk-go-v2/aws/signer/v4`
- `github.com/aws/aws-sdk-go-v2/aws/transport/http`
- `github.com/aws/aws-sdk-go-v2/config`
- `github.com/aws/aws-sdk-go-v2/credentials`
- `github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/endpointcreds/internal/client`
- `github.com/aws/aws-sdk-go-v2/credentials/logincreds`
- `github.com/aws/aws-sdk-go-v2/credentials/processcreds`
- `github.com/aws/aws-sdk-go-v2/credentials/ssocreds`
- `github.com/aws/aws-sdk-go-v2/credentials/stscreds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds`
- `github.com/aws/aws-sdk-go-v2/feature/ec2/imds/internal/config`
- `github.com/aws/aws-sdk-go-v2/internal/auth`
- `github.com/aws/aws-sdk-go-v2/internal/auth/smithy`
- `github.com/aws/aws-sdk-go-v2/internal/configsources`
- `github.com/aws/aws-sdk-go-v2/internal/context`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/awsrulesfn`
- `github.com/aws/aws-sdk-go-v2/internal/endpoints/v2`
- `github.com/aws/aws-sdk-go-v2/internal/ini`
- `github.com/aws/aws-sdk-go-v2/internal/middleware`
- `github.com/aws/aws-sdk-go-v2/internal/rand`
- `github.com/aws/aws-sdk-go-v2/internal/sdk`
- `github.com/aws/aws-sdk-go-v2/internal/sdkio`
- `github.com/aws/aws-sdk-go-v2/internal/shareddefaults`
- `github.com/aws/aws-sdk-go-v2/internal/strings`
- `github.com/aws/aws-sdk-go-v2/internal/sync/singleflight`
- `github.com/aws/aws-sdk-go-v2/internal/timeconv`
- `github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding`
- `github.com/aws/aws-sdk-go-v2/service/internal/presigned-url`
- `github.com/aws/aws-sdk-go-v2/service/signin`
- `github.com/aws/aws-sdk-go-v2/service/signin/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/signin/types`
- `github.com/aws/aws-sdk-go-v2/service/sso`
- `github.com/aws/aws-sdk-go-v2/service/sso/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sso/types`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/ssooidc/types`
- `github.com/aws/aws-sdk-go-v2/service/sts`
- `github.com/aws/aws-sdk-go-v2/service/sts/internal/endpoints`
- `github.com/aws/aws-sdk-go-v2/service/sts/types`
- `github.com/aws/smithy-go`
- `github.com/aws/smithy-go/auth`
- `github.com/aws/smithy-go/auth/bearer`
- `github.com/aws/smithy-go/context`
- `github.com/aws/smithy-go/document`
- `github.com/aws/smithy-go/encoding`
- `github.com/aws/smithy-go/encoding/httpbinding`
- `github.com/aws/smithy-go/encoding/json`
- `github.com/aws/smithy-go/encoding/xml`
- `github.com/aws/smithy-go/endpoints`
- `github.com/aws/smithy-go/endpoints/private/rulesfn`
- `github.com/aws/smithy-go/internal/sync/singleflight`
- `github.com/aws/smithy-go/io`
- `github.com/aws/smithy-go/logging`
- `github.com/aws/smithy-go/metrics`
- `github.com/aws/smithy-go/middleware`
- `github.com/aws/smithy-go/private/requestcompression`
- ... (1191 more)

**Stdlib deps**

- 239 packages

## `github.com/kubekattle/ktl/pkg/api/ktl/api/v1`

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

## `github.com/kubekattle/ktl/pkg/buildkit`

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
- `github.com/google/go-containerregistry/pkg/v1/types`
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
- ... (245 more)

**Stdlib deps**

- 210 packages

## `github.com/kubekattle/ktl/pkg/compose`

**Internal deps**

- `github.com/kubekattle/ktl/internal/csvutil`
- `github.com/kubekattle/ktl/pkg/buildkit`
- `github.com/kubekattle/ktl/pkg/registry`

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

## `github.com/kubekattle/ktl/pkg/registry`

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

