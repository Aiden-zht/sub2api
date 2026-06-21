# Minimal Clean Theme

This is the smallest installable Sub2API theme package used to validate the admin theme upload flow.

## Package Shape

Zip the files in this directory so `manifest.json` is at the zip root:

```bash
cd examples/themes/minimal-clean
zip -r /tmp/minimal-clean-theme.zip manifest.json theme.css
```

Upload `/tmp/minimal-clean-theme.zip` from **Admin / Themes**, then enable `minimal-clean`.

## Rules

- `manifest.json` is required at the zip root.
- `entry` must point to a CSS file.
- Static assets such as CSS, images, icons, and `woff2` fonts are allowed.
- Documentation files such as this `README.md` are for the source repository only and should not be included in the upload zip.
- JavaScript, Vue, HTML, WASM, shell scripts, and native binaries are rejected.
- The first theme API version is `"1"`.
