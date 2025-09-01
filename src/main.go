package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"iter"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/goccy/go-yaml"
)

type Config struct {
	RefreshRate       int    `yaml:"refresh_rate"`
	WebhookURL        string `yaml:"webhook_url"`
	WebhookFormat     string `yaml:"webhook_format"`
	PatternFolder     string `yaml:"pattern_folder"`
	FolderRefreshRate int    `yaml:"folder_refresh_rate"`
	RemindTime        int    `yaml:"remind_time"`
}

type Pattern struct {
	Name string
	Data image.Image
	X    int
	Y    int
	Tile
}
type PatternInfo struct {
	Pattern
	DefacedSince time.Time
	Errors       int
}

type PatternPos struct {
	X, Y int
	Tile
}

var config Config
var patterns map[PatternPos]*PatternInfo
var webhookFormat string

type TemplateStruct struct {
	DefacedSince time.Time
	Errors       int
	ErrorsBefore int
	PatternName  string
	PatternPos   PatternPos
}

type ComparedPattern struct {
	Pattern
	Errored int
}

type Tile struct {
	Tx, Ty int
	Data   *image.Image
}

func NewTile(Tx, Ty int) Tile {
	return Tile{Tx, Ty, nil}
}

func GetImage(tile Tile) (Tile, error) {
	res, err := http.Get(fmt.Sprintf("https://backend.wplace.live/files/s0/tiles/%d/%d.png", tile.Tx, tile.Ty))
	if err != nil {
		return tile, err
	}

	defer res.Body.Close()
	img, err := png.Decode(res.Body)
	if err != nil {
		return tile, err
	}
	return Tile{tile.Tx, tile.Ty, &img}, nil
}

func ComputeNecessaryTilesForPattern(pattern Pattern, needed map[Tile]*Tile) {
	width := pattern.Data.Bounds().Dx()
	height := pattern.Data.Bounds().Dy()
	for Tx := pattern.Tx; Tx <= pattern.Tx+(pattern.X+width)/1000; Tx++ {
		for Ty := pattern.Ty; Ty <= pattern.Ty+(pattern.Y+height)/1000; Ty++ {
			tile := NewTile(Tx, Ty)
			needed[tile] = &tile
		}
	}
}

func Compare(patterns iter.Seq[Pattern], needed map[Tile]Tile) map[Pattern]*ComparedPattern {
	var compared map[Pattern]*ComparedPattern
	compared = make(map[Pattern]*ComparedPattern)
	for pattern := range patterns {
		compared[pattern] = &ComparedPattern{pattern, 0}
		width := pattern.Data.Bounds().Dx()
		height := pattern.Data.Bounds().Dy()
		for x := 0; x < width; x++ {
			for y := 0; y < height; y++ {
				// Get tile associated with this pixel
				tileId := NewTile(pattern.Tx+(x+pattern.X)/1000, pattern.Ty+(y+pattern.Y)/1000)
				if tile, ok := needed[tileId]; ok {
					if tile.Data != nil {
						r, g, b, a := (*tile.Data).At((x+pattern.X)%1000, (y+pattern.Y)%1000).RGBA()

						rp, gp, bp, ap := pattern.Data.At(x, y).RGBA()
						if ap == 0 {
							continue
						}
						if r != rp || g != gp || b != bp || a != ap {
							if cmp, ok := compared[pattern]; ok {
								cmp.Errored++
							} else {
								compared[pattern] = &ComparedPattern{pattern, 1}
							}
						}
					} else {
						// error
					}
				} else {
					// error
				}
			}
		}
	}
	return compared
}

func UpdatePatterns(folder string) {
	entries, err := os.ReadDir(config.PatternFolder)
	if err != nil {
		log.Fatalln("Unable to open pattern folder", err)
	}
	var newPatterns map[PatternPos]*PatternInfo
	newPatterns = make(map[PatternPos]*PatternInfo)
	for _, e := range entries {
		bits := strings.Split(e.Name(), ".")
		if len(bits) != 6 {
			log.Printf("[WARNING] Malformed pattern name %s\n", e.Name())
			continue
		}
		Tx, err := strconv.Atoi(bits[1])
		Ty, err := strconv.Atoi(bits[2])
		x, err := strconv.Atoi(bits[3])
		y, err := strconv.Atoi(bits[4])
		if err != nil {
			log.Printf("[WARNING] Malformed pattern name %s\n", e.Name())
			continue
		}
		reader, err := os.Open(path.Join(config.PatternFolder, e.Name()))
		if err != nil {
			log.Printf("[WARNING] Unable to read pattern file %s\n", e.Name())
			continue
		}
		data, err := png.Decode(reader)
		reader.Close()
		if err != nil {
			log.Printf("[WARNING] Unable to decode pattern file %s\n", e.Name())
			continue
		}

		ppos := PatternPos{X: x, Y: y, Tile: NewTile(Tx, Ty)}
		p := Pattern{Name: bits[0], Data: data, X: x, Y: y, Tile: NewTile(Tx, Ty)}
		if pi, ok := patterns[ppos]; ok {
			pi.Pattern = p
			newPatterns[ppos] = pi

		} else {
			newPatterns[ppos] = &PatternInfo{Errors: 0, DefacedSince: time.Now(), Pattern: p}

		}
	}
	patterns = newPatterns
}

func init() {
	file, err := os.ReadFile(os.Getenv("CONFIG_FILE"))
	if err != nil {
		log.Fatalln("Unable to open config", err)
	}
	err = yaml.Unmarshal(file, &config)
	if err != nil {
		log.Fatalln(err)
	}
	content, err := os.ReadFile(config.WebhookFormat)
	if err != nil {
		log.Fatalln("Unable to open webhook format", err)
	}
	webhookFormat = string(content)
	fmt.Println("[INFO] Config parsed")
	patterns = make(map[PatternPos]*PatternInfo)
	UpdatePatterns(config.PatternFolder)
	fmt.Println("[INFO] Patterns parsed")
}

func main() {
	refreshTicker := time.Tick(time.Duration(config.RefreshRate) * time.Second)
	refreshFolderTicker := time.Tick(time.Duration(config.FolderRefreshRate) * time.Second)
	fmt.Println("[INFO] Server started")
	for {
		select {
		case <-refreshFolderTicker:
			log.Println("[INFO] Refreshing folder")
			UpdatePatterns(config.PatternFolder)
		case <-refreshTicker:
			log.Println("[INFO] Refreshing patterns")
			var needed map[Tile]*Tile
			needed = make(map[Tile]*Tile)
			for _, pinfo := range patterns {
				ComputeNecessaryTilesForPattern(pinfo.Pattern, needed)
			}
			var fetchedTiles map[Tile]Tile
			fetchedTiles = make(map[Tile]Tile)
			for tile := range needed {
				newTile, err := GetImage(tile)
				if err != nil {
					return
				}
				fetchedTiles[tile] = newTile
			}
			f := func(yield func(Pattern) bool) {
				for _, k := range patterns {
					if !yield(k.Pattern) {
						return
					}
				}
			}
			compared := Compare(f, fetchedTiles)
			for pt, cmp := range compared {
				log.Println(pt, cmp)
				ppos := PatternPos{X: pt.X, Y: pt.Y, Tile: pt.Tile}
				if pi, ok := patterns[ppos]; ok {
					if pi.Errors < cmp.Errored ||
						(pi.Errors != 0 && cmp.Errored == 0) ||
						(pi.Errors != 0 && pi.DefacedSince.Add(time.Second*time.Duration(config.RemindTime)).Before(time.Now())) {
						pi.DefacedSince = time.Now()
						ts := TemplateStruct{
							DefacedSince: pi.DefacedSince,
							Errors:       cmp.Errored,
							ErrorsBefore: pi.Errors,
							PatternName:  pt.Name,
							PatternPos:   ppos,
						}
						pi.Errors = cmp.Errored
						tmp, err := template.New("template").Parse(webhookFormat)
						if err != nil {
							log.Printf("[ERROR] Unable to parse template: %s\n", err)
						}
						w := bytes.NewBuffer(nil)
						err = tmp.Execute(w, ts)
						if err != nil {
							log.Printf("[ERROR] Unable to execute template: %s\n", err)
						}
						res, err := http.Post(config.WebhookURL, "application/json", bytes.NewBuffer(w.Bytes()))
						if err != nil {
							log.Printf("[ERROR] Unable to send webhook %d\n", res.StatusCode)
						}
						if res.StatusCode != http.StatusNoContent {
							log.Printf("[ERROR] Invalid status code (Unable to send webhook) %d\n", res.StatusCode)
							buffer := bytes.NewBuffer(nil)
							_, _ = buffer.ReadFrom(res.Body)
							log.Println("error message :", string(buffer.Bytes()))
						}
					}
					pi.Errors = cmp.Errored
				} else {
					panic("unable to find pattern in patterns")
				}
			}
		}
	}
}
