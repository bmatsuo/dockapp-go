A simple, customizable battery indicator dockapp for Openbox.

##Examples

For a minimal, small window overlay the battery and text.  The `{{.durShort
.remaining}}` template is useful for rendering remaining time in a small space.

```sh
dockapp-battery \
    -window.geometry  '40x20' \
    -battery.geometry '38x18+1+1' \
    -text.geometry    '38x18+1+1' \
    '{{.percent}}' \
    '{{durShort .remaining}}'
```
