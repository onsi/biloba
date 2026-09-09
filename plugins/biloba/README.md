# Biloba compatibility plugin

This deprecated plugin preserves the original `biloba:*` skill namespace for existing installations. Every skill is a link to the canonical `biloba-go` implementation; there is no second copy to maintain.

New installations should use:

```
/plugin install biloba-go@biloba
```

## Do not install both

This plugin and `biloba-go` serve the *same ten skill files*. Installing both lists every skill twice, with byte-identical descriptions and nothing to choose between them — an agent picking one is flipping a coin, and the duplicates are pure listing noise.

Migrate by uninstalling this plugin and installing `biloba-go`, rather than adding the new one alongside it. Existing `biloba@biloba` installations can keep updating during the transition window; the compatibility plugin will be removed after the announced transition period.

## A note on symlinks

The ten entries under `skills/` are git symlinks (mode `120000`) into `../../biloba-go/skills/`. On a checkout that does not materialize symlinks — Windows without developer mode, or `core.symlinks=false` — each becomes a regular file whose contents are the path string, and all ten `biloba:*` skills silently disappear. `scripts/check-plugins.sh` asserts every entry is a real symlink that resolves to a `SKILL.md`, so the repo's own CI catches a broken checkout; if your `biloba:*` skills vanish after cloning, check this first.
