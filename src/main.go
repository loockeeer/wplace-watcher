package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
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
	RefreshRate          int    `yaml:"refresh_rate"`
	WebhookURL           string `yaml:"webhook_url"`
	WebhookFormat        string `yaml:"webhook_format"`
	PatternDirectory     string `yaml:"pattern_directory"`
	DirectoryRefreshRate int    `yaml:"directory_refresh_rate"`
	RemindTime           int    `yaml:"remind_time"`
}

type Position struct {
	X, Y int
}
type Pattern struct {
	Name         string
	Data         image.Image
	Errors       int
	DefacedSince time.Time
	Tile         Position
	TilePosition Position
}

type TemplateStruct struct {
	Errors       int
	ErrorsBefore int
	PatternName  string
	PatternTile  Position
	PatternPos   Position
}

type ExpectedCellData struct {
	color.RGBA
	patternName string
}
type ExpectedTileData = [1000][1000]ExpectedCellData

var config Config
var patterns map[string]Pattern
var webhookTemplate *template.Template
var needed map[Position]ExpectedTileData

// Updates every pattern from the pattern directory
func UpdatePatterns(directory string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		log.Fatalln("Unable to open pattern directory", err)
	}
	newPatterns := make(map[string]Pattern)
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
		reader, err := os.Open(path.Join(directory, e.Name()))
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

		if oldPattern, ok := patterns[bits[0]]; ok {
			oldPattern.Tile = Position{Tx, Ty}
			oldPattern.TilePosition = Position{x, y}
			oldPattern.Data = data
			newPatterns[bits[0]] = oldPattern
		} else {
			newPatterns[bits[0]] = Pattern{bits[0], data, 0, time.Now(), Position{Tx, Ty}, Position{x, y}}
		}
	}
	patterns = newPatterns
}

// Computes the tile masks; i.e. for every tile that at least one (1) pattern covers, the pixels that should (according to the pattern(s)) be there
func ComputeTileMasks(patterns map[string]Pattern) map[Position]ExpectedTileData {
	out := make(map[Position]ExpectedTileData)
	for _, pattern := range patterns {
		width := pattern.Data.Bounds().Dx()
		height := pattern.Data.Bounds().Dy()
		for Tx := pattern.Tile.X; Tx <= pattern.Tile.X+(pattern.TilePosition.X+width)/1000; Tx++ {
			for Ty := pattern.Tile.Y; Ty <= pattern.Tile.Y+(pattern.TilePosition.Y+height)/1000; Ty++ {
				pos := Position{Tx, Ty}
				if etd, ok := out[pos]; ok {
					for x := Tx * 1000; x < min((Tx+1)*1000, width); x++ {
						for y := Ty * 1000; y < min((Ty+1)*1000, height); y++ {
							etd[x][y].patternName = pattern.Name
							etd[x][y].RGBA = pattern.Data.At(x, y).(color.RGBA)
						}
					}
				}
			}
		}
	}
	return out
}

// Fetches tile image associated with a position from wplace's tile servers
func FetchTileImage(pos Position) (image.Image, error) {
	res, err := http.Get(fmt.Sprintf("https://backend.wplace.live/files/s0/tiles/%d/%d.png", pos.X, pos.Y))
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	img, err := png.Decode(res.Body)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// Compares the provided tiles masks to the fetched tile images and reports which drawings have been defaced, and by how much
func CompareTileMasks(tiles map[Position]image.Image, tileMask map[Position]ExpectedTileData) map[string]int {
	out := make(map[string]int)
	for pos, mask := range tileMask {
		tile, ok := tiles[pos]
		if !ok {
			log.Println("[WARNING] Missing tile")
		}
		for x := 0; x < 1000; x++ {
			for y := 0; y < 1000; y++ {
				if mask[x][y].A == 0 {
					if mask[x][y].RGBA != tile.At(x, y).(color.RGBA) {
						if _, ok := out[mask[x][y].patternName]; !ok {
							out[mask[x][y].patternName] = 1
						} else {
							out[mask[x][y].patternName]++
						}
					}
				}
			}
		}
	}
	return out
}

// Sends defacement updates through the provided webhook
func SendUpdates(patterns map[string]Pattern, errorsMap map[string]int) {
	for _, pattern := range patterns {
		patternErrors, ok := errorsMap[pattern.Name]
		if !ok {
			patternErrors = 0
		}
		// if more errors are found, generate a webhook body and send it
		// if there are no errors (after there being some) ...
		// after remind delay, ...
		errorsBefore := pattern.Errors
		pattern.Errors = patternErrors
		log.Printf("[INFO] Pattern (%s) found with (%d)->(%d) errors\n", pattern.Name, errorsBefore, patternErrors)
		if (patternErrors > errorsBefore) ||
			(patternErrors == 0 && errorsBefore != 0) ||
			time.Now().Add(time.Duration(config.RemindTime)).Before(time.Now()) {
			log.Printf("[INFO] Sending webhook for pattern (%s)\n", pattern.Name)
			pattern.DefacedSince = time.Now()
			ts := TemplateStruct{
				Errors:       patternErrors,
				ErrorsBefore: errorsBefore,
				PatternName:  pattern.Name,
				PatternTile:  pattern.Tile,
				PatternPos:   pattern.TilePosition,
			}
			tmp, err := webhookTemplate.Clone()
			if err != nil {
				log.Println("[ERROR] Unable to clone template :", err)
				continue
			}
			w := bytes.NewBuffer(nil)
			err = tmp.Execute(w, ts)
			if err != nil {
				log.Println("[ERROR] Unable to execute template :", err)
				continue
			}
			res, err := http.Post(config.WebhookURL, "application/json", bytes.NewBuffer(w.Bytes()))
			if err != nil {
				if res != nil {
					log.Println("[ERROR] Unable to send webhook :", res.StatusCode)
				} else {
					log.Println("[ERROR] Unable to send webhook : [nil response]")
				}
				continue
			}
			if res.StatusCode != http.StatusNoContent {
				log.Printf("[ERROR] Invalid status code (Unable to send webhook) %d\n", res.StatusCode)
				buffer := bytes.NewBuffer(nil)
				_, _ = buffer.ReadFrom(res.Body)
				log.Println("error message :", string(buffer.Bytes()))
			}
		}
	}
}

// Config parsing, webhook template parsing and initial pattern list creation
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
	webhookTemplate, err = template.New("template").Parse(string(content))
	if err != nil {
		log.Fatalf("[ERROR] Unable to parse template: %s\n", err)
	}
	log.Println("[INFO] Config parsed")
	patterns = make(map[string]Pattern)
	UpdatePatterns(config.PatternDirectory)
	log.Println("[INFO] Patterns parsed")
}

// Main loop
func main() {
	refreshTicker := time.Tick(time.Duration(config.RefreshRate) * time.Second)
	refreshDirTicker := time.Tick(time.Duration(config.DirectoryRefreshRate) * time.Second)
	log.Println("[INFO] Server started")
	for {
		select {
		case <-refreshDirTicker:
			log.Println("[INFO] Refreshing directory (1/2)")
			UpdatePatterns(config.PatternDirectory)
			log.Println("[INFO] Computing tile masks (2/2)")
			needed = ComputeTileMasks(patterns)
			log.Println("[INFO] Done with directory refresh")
		case <-refreshTicker:
			log.Println("[INFO] Refreshing patterns")

			fetchedTiles := make(map[Position]image.Image)
			for pos, _ := range needed {
				img, err := FetchTileImage(pos)
				if err != nil {
					log.Printf("[ERROR] Unable to fetch tile (%d,%d) : %s\n", pos.X, pos.Y, err)
				}
				fetchedTiles[pos] = img
			}
			compared := CompareTileMasks(fetchedTiles, needed)
			SendUpdates(patterns, compared)
		}
	}
}
