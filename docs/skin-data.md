# How Bedrock actually delivers a skin

Everything here was established by capturing real login traffic from a Minecraft Bedrock client, not from documentation. If you are writing a proxy, a server, or anything that receives skins from live players, this is the page that will save you the most time.

## Where a skin lives

A Bedrock client announces its skin in the **Login packet**, before it has joined anything. In [gophertunnel](https://github.com/sandertv/gophertunnel) terms that is `login.ClientData`, reachable as `conn.ClientData()` the moment a connection is accepted.

Nothing needs to be forwarded to a real server to obtain a skin. Accepting the connection is enough — the client has already told you everything.

The fields that matter:

| Field | What it holds |
| --- | --- |
| `SkinData` | The texture, base64 of **raw RGBA bytes** — not a PNG |
| `SkinImageWidth` / `SkinImageHeight` | Dimensions for those bytes |
| `SkinGeometryData` | base64 of `geometry.json` — **usually literally `null`** |
| `SkinResourcePatch` | base64 JSON naming which model to use |
| `CapeData`, `CapeImageWidth/Height` | Cape texture, same raw-RGBA encoding |
| `SkinID` | e.g. `c18e65aa-…-8ad63622ef01.Steve` |
| `PersonaSkin` | Whether this is an avatar-builder skin |
| `ArmSize` | `"wide"` or `"slim"` — **do not trust this**, see below |

## The texture is not an image file

`SkinData` decodes to `width * height * 4` bytes of non-premultiplied RGBA. There is no header, no signature, no dimensions inside it — that is what the separate width/height fields are for.

Since that is exactly `image.NRGBA`'s memory layout, the byte slice can back an image directly with no per-pixel copying:

```go
raw, err := base64.StdEncoding.DecodeString(data.SkinData)
if err != nil {
	return err
}
if len(raw) != data.SkinImageWidth*data.SkinImageHeight*4 {
	return fmt.Errorf("got %d bytes, expected %d", len(raw), data.SkinImageWidth*data.SkinImageHeight*4)
}

img := &image.NRGBA{
	Pix:    raw,
	Stride: data.SkinImageWidth * 4,
	Rect:   image.Rect(0, 0, data.SkinImageWidth, data.SkinImageHeight),
}
```

Check that length. If the byte count disagrees with the declared dimensions you want a clear error, not a silently garbled image.

Typical sizes are 64×64, 64×32 (legacy), and 128×128 (high resolution).

## Most skins send no geometry at all

This is the important one.

`SkinGeometryData` base64-decodes, for a stock skin, to the four bytes:

```
null
```

Sometimes with a trailing newline — `"null\n"` — so trim before comparing.

That is not an error and not an empty skin. A client using one of the **built-in models** sends no mesh, because both ends already have it. All it sends is the resource patch naming which built-in model applies:

```json
{ "geometry": { "default": "geometry.humanoid.custom" } }
```

Geometry data only travels the wire for skins with a genuinely custom mesh — the ones with ears, tails, wings, or unusual proportions.

The practical consequences:

- A skin-rendering service that **requires** geometry rejects the most common skin in the game.
- `IsEmpty` exists precisely to separate "this skin has no custom mesh" from "this upload is broken":

  ```go
  if skinapi.IsEmpty(raw) {
      // Use the built-in model.
      geos = nil
  } else {
      geos, err = skinapi.ParseGeometry(raw)
      if err != nil {
          return err // genuinely malformed, not just absent
      }
  }
  ```

- `ParseGeometry([]byte("null"))` deliberately succeeds and returns zero entries rather than erroring, so passing the field straight through is safe.
- `Render` with `Geometry: nil` uses `DefaultGeometry()`, which is the vanilla humanoid bundle captured from a real client. See [design-decisions.md](design-decisions.md#why-the-default-geometry-is-embedded).

## The resource patch is the authoritative model selector

`SkinResourcePatch` decodes to JSON of this shape:

```json
{ "geometry": { "default": "geometry.humanoid.customSlim" } }
```

That `default` value is what to pass as `Options.Identifier`.

**Do not use `ArmSize` for this.** Real captures show the two disagreeing: a skin reporting `ArmSize: "wide"` whose resource patch names `geometry.humanoid.customSlim`. The patch is what the client renders from. `ArmSize` is at best a hint and at worst wrong.

```go
var patch struct {
	Geometry struct {
		Default string `json:"default"`
	} `json:"geometry"`
}
raw, _ := base64.StdEncoding.DecodeString(data.SkinResourcePatch)
json.Unmarshal(raw, &patch)

img, err := skinapi.Render(skinapi.Options{
	Texture:    tex,
	Geometry:   geos,                   // nil is fine
	Identifier: patch.Geometry.Default, // "" is also fine
})
```

An `Identifier` that matches nothing is not an error — `SelectGeometry` falls back to the entry with the most cubes.

## Persona skins send nothing renderable

When `PersonaSkin` is true, the skin was assembled in the avatar builder from separate pieces. Bedrock never sends real mesh data for these. What arrives is geometry with real, named bones and **zero cubes in all of them**.

There is genuinely nothing to rasterize. `Render` detects this — `Geometry.TotalCubes() == 0` — and falls back to a flat texture crop rather than failing. See [design-decisions.md](design-decisions.md#why-persona-skins-fall-back-to-2d).

## Capes live in their own entry

A cape is never merged into the body geometry. It arrives as a separate entry, conventionally `geometry.cape`, holding a small self-contained bone chain — typically `body` → `waist` → `cape`, where only `cape` has a cube (10×16×1).

`FindCape` looks for any entry containing a bone literally named `cape` that actually has a cube. The cape texture itself is a separate `CapeData` field with its own dimensions, usually 64×32.

Both must be present for a cape to render: geometry with a cape bone, and a cape texture.

## A minimal capture server

Roughly 40 lines gets you every skin that connects:

```go
cfg := minecraft.ListenConfig{
	StatusProvider:         minecraft.NewStatusProvider("skin capture", ""),
	AuthenticationDisabled: true, // local capture box
}
listener, err := cfg.Listen("raknet", "0.0.0.0:19132")
if err != nil {
	return err
}
defer listener.Close()

for {
	c, err := listener.Accept()
	if err != nil {
		return err
	}
	conn := c.(*minecraft.Conn)
	data := conn.ClientData()

	// ... decode SkinData, SkinGeometryData, SkinResourcePatch here ...

	_ = listener.Disconnect(conn, "Captured, thanks.")
}
```

Point a Bedrock client at it as a server on port 19132 and join. The login packet arrives before anything else, so the client never needs to spawn.

**If you save captures, keep them out of version control.** `clientdata.json` contains the player's XUID, gamertag and device ID.

## Field summary for the impatient

- `SkinData` is raw RGBA, not PNG. Use the separate width/height.
- `SkinGeometryData` is usually `null`. That is normal.
- `SkinResourcePatch`, not `ArmSize`, decides wide vs slim.
- `SkinGeometryDataEngineVersion` is *also* base64 — `MC4wLjA=` is just `0.0.0`. Easy to miss when every neighbouring field of that shape holds JSON.
- Persona skins have bones but no cubes.
- Capes are a separate geometry entry plus a separate texture.
