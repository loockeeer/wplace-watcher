## **This is some of the messiest code I've ever written, and is planned to be overhauled. Expect changes (and updates!)**

# wplacer-watcher

Drawing on the WPlace canvas can be... hard and long at times. Especially when griefers come to annoy.
This project should help those with many projects they wish to defend against griefing !
Every time one of the referenced drawings is griefed, a message is sent to a WebHook, allowing for customisation and easy translation.
When the drawing is restored, it is possible to have another message sent to you !

## Requirements

- Docker 
- Git

## Deployment

After pulling the repository, choose a webhook format example (or make your own, see lower) from the `examples` directory, and copy it to `./webhook_format.json.tmpl` in the project's root directory.
Make sure to create a `config.yaml` file containing :
```yaml
refresh_rate: 5 # The amount in seconds the app will wait between each grief check
webhook_url: "SUPER DUPER SECRET" # Your Webhook URL
webhook_format: "/data/webhook_format.json.tmpl" # Usually unchanged 
pattern_directory: "/data/patterns" # Usually unchanged
directory_refresh_rate: 120 # The amount in seconds the app will wait between each patterns directory refresh
remind_time: 18000 # The amount in seconds the app will wait before remind you a drawing was griefed, if it hasn't been fixed before
```

Simply run `docker compose up -d` and your service should now be running, but how do you use it?

### Config
If you are looking to create your own Webhook message, note that I won't provide any tutorial apart from this :
- The webhook format language is Golang's template engine, which is LARGELY documented on the internet
- These variables can be used
```yaml
Errors       int # The number of wrong pixels
ErrorsBefore int # The number of wrong pixels , *before* the alert Was triggered (usually 0, can be non-zero if the trigger is due to further damage)
PatternName  string # The pattern's name
PatternPos   PatternPos # The pattern's position (PatternPos.Tx,.Ty,.X,.Y are all integers)
```
You can always assume that `Errors` being `0` means that the drawing was refaced.
Not sending a message through the Webhook is not «officially» supported, but any invalid message you generate will only result in a warning/error message and won't crash the app, since any non-ok HTTP code is non-fatal.
## Usage
### Patterns
Patterns to watch for should be placed in the `patterns` directory, following a strict naming convention :
```
NAME.Tx.Ty.x.y.png
```
Where, 
- `NAME` is a custom name used to identify this pattern. Not necessarily unique
- `Tx`, `Ty`, `x`, `y` are the coordinates (in order) where your pattern (its top left corner) is placed. These coordinates are often found using Blue Marble 

After waiting a short (or long) while (depending on your `directory_refresh_rate` setting ! Which I would avoid setting to 0 or 1 so as to avoid strain on your equipment), the software will have taken notice of the new pattern !
Removing a pattern from the watchlist is as simple as deleting it, or moving it out of the directory.

In order to allow for gaps in your drawings, transparency is required, and the format I chose for this project is png.
This likely won't change, and in the event of you placing a non-png encoded file in the directory, it would not be read properly. Note that this wouldn't crash the server though.