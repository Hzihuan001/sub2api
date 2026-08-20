# Third-Party Notices

This customized Sub2API distribution incorporates work from the following
public forks. The repository's `LICENSE` file contains the GNU Lesser General
Public License, version 3, that applies to these contributions.

## Kiro platform integration

- Source: <https://github.com/nianzs/sub2api>
- Integrated revision: `75623a9a19427cbe69c9533310a2e524ea5d0d2a`
- License in source repository: GNU Lesser General Public License v3.0

The integrated work includes Kiro account authentication, token refresh,
gateway translation, usage and cooldown handling, administration UI, tests,
and database migrations.

## Cursor platform integration

- Source: <https://github.com/SJwen0/cursor-->
- Integrated revision: `3709f0f6c83ed84b62c2a0f7f8e1ff63d6cfb7d4`
- License in source repository: GNU Lesser General Public License v3.0

The integrated work includes Cursor account authentication, token refresh,
Connect/protobuf gateway translation, model discovery, administration UI,
tests, operational documentation, and database migration 222.

Local integration changes combine these platform implementations with the
Operator authorization layer and the Sub2API v0.1.179 custom baseline.
