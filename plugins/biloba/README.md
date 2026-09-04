# Biloba compatibility plugin

This deprecated plugin preserves the original `biloba:*` skill namespace for existing installations. Every skill is a link to the canonical `biloba-gomega` implementation; there is no second copy to maintain.

New installations should use:

```
/plugin install biloba-gomega@biloba
```

Existing `biloba@biloba` installations can continue to update during the transition window, but should migrate by uninstalling `biloba@biloba` and installing `biloba-gomega@biloba`. The compatibility plugin will be removed after the announced transition period.
