# OC notification fork

Source: github.com/rjeczalik/notify v0.9.3. Windows production Go files are copied
with Windows-only build constraints and corrected Windows notification handling.
Other platforms delegate to the unmodified upstream module through type aliases.
The upstream MIT license and source attribution are retained.

OC uses this internal copy because upstream v0.9.3 and commit
6f12d5684fcf still convert a filename into a MAX_LONG_PATH array outside the
notification buffer, which crashes Windows race tests with checkptr enabled.
The decoder reads bounded little-endian fields and UTF-16 code units instead.
Kernel overflow, malformed notifications and read failures propagate a loss event
to all affected subscriptions. OC reports WatchEventsLost and mirror reconciles.
Stop closes idle directory handles immediately; late cancellation completions
cannot remove a new subscription registered at the same path.
Its platform-independent regression tests run on every CI platform.

The Windows fork is part of OC's main module, so its Go module SBOM does not list
it as a separate dependency. Preserve this directory and LICENSE in source releases;
include its license in binary release archives and installation packages.
