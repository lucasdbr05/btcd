#!/bin/sh

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
version_file="$script_dir/VERSION"
package=libbitcoinkernel

fail() {
	printf 'libbitcoinkernel verification failed: %s\n' "$*" >&2
	exit 1
}

command -v pkg-config >/dev/null 2>&1 || fail "pkg-config was not found"
command -v cc >/dev/null 2>&1 || fail "a C compiler named 'cc' was not found"

expected_commit=$(sed -n 's/^BITCOIN_CORE_COMMIT=//p' "$version_file")
expected_header_sha256=$(sed -n 's/^BITCOINKERNEL_HEADER_SHA256=//p' "$version_file")

[ -n "$expected_commit" ] || fail "BITCOIN_CORE_COMMIT is missing from VERSION"
[ -n "$expected_header_sha256" ] || fail "BITCOINKERNEL_HEADER_SHA256 is missing from VERSION"

pkg-config --exists "$package" || fail \
	"pkg-config cannot find $package; set PKG_CONFIG_PATH to its pkgconfig directory"

include_dir=$(pkg-config --variable=includedir "$package")
lib_dir=$(pkg-config --variable=libdir "$package")
header="$include_dir/bitcoinkernel.h"

[ -f "$header" ] || fail "installed header was not found at $header"
[ -n "$lib_dir" ] || fail "pkg-config did not report a library directory"

if command -v sha256sum >/dev/null 2>&1; then
	header_sha256=$(sha256sum "$header" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	header_sha256=$(shasum -a 256 "$header" | awk '{print $1}')
else
	fail "neither sha256sum nor shasum was found"
fi

[ "$header_sha256" = "$expected_header_sha256" ] || fail \
	"header SHA-256 mismatch (expected $expected_header_sha256, got $header_sha256)"

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/btcd-libbitcoinkernel.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

cat >"$tmp_dir/smoke.c" <<'EOF'
#include <bitcoinkernel.h>

int main(void)
{
    btck_ChainParameters* chain_params =
        btck_chain_parameters_create(btck_ChainType_REGTEST);
    if (chain_params == NULL) return 1;

    btck_ContextOptions* options = btck_context_options_create();
    if (options == NULL) {
        btck_chain_parameters_destroy(chain_params);
        return 2;
    }

    btck_context_options_set_chainparams(options, chain_params);
    btck_Context* context = btck_context_create(options);

    btck_context_options_destroy(options);
    btck_chain_parameters_destroy(chain_params);

    if (context == NULL) return 3;
    btck_context_destroy(context);
    return 0;
}
EOF

# Word splitting is required here because pkg-config emits a list of compiler
# and linker arguments.  Neither output may contain shell control operators.
# shellcheck disable=SC2046
cc $(pkg-config --cflags "$package") "$tmp_dir/smoke.c" \
	-o "$tmp_dir/smoke" $(pkg-config --libs "$package")

case $(uname -s) in
	Darwin)
		DYLD_LIBRARY_PATH="$lib_dir${DYLD_LIBRARY_PATH:+:$DYLD_LIBRARY_PATH}"
		export DYLD_LIBRARY_PATH
		"$tmp_dir/smoke"
		;;
	Linux)
		LD_LIBRARY_PATH="$lib_dir${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
		export LD_LIBRARY_PATH
		"$tmp_dir/smoke"
		;;
	*)
		"$tmp_dir/smoke"
		;;
esac

printf '%s\n' \
	"libbitcoinkernel installation verified" \
	"  Bitcoin Core commit: $expected_commit" \
	"  pkg-config version:  $(pkg-config --modversion "$package")" \
	"  header:              $header" \
	"  library directory:   $lib_dir"
