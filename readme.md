# Indigo
The Miiverse clone to end all Miiverse clones, for real this time. **NOTICE:** This code is WIP and is only being posted to fix errors more easily.
## What is this?
See [the FAQ](https://indigo.cafe/help/faq) if you have any questions about this. Long story short, this is the most recent in a long line of similar-looking social networks known as "[Miiverse](https://miiverse.nintendo.net/) clones" which was created for speed, new features, and getting away from the bad ownership and administration of the previous iterations.
## Installation
1. Install the latest version of Go from [golang.org](https://golang.org/dl/) or from the official gopher repository.
2. Download the .ZIP file or clone the repository like any other project.
3. Install all the dependencies with `go get ./...`.
4. Set up a MySQL server and import structure.sql.
5. Modify the config.json file to your liking.
6. Optional: If you want to install GeoIP (necessary for user timezones to be correct and getting user regions), [download a GeoLite database from MaxMind](https://geolite.maxmind.com/download/geoip/database/GeoLite2-City.tar.gz), unzip the file and rename it geoip.mmdb and put in the same folder as main.go.
7. Build the server with `go build` and then run the new program that is created, or use `go run *.go` (Linux/MacOS only).
8. Make an account, give yourself admin through the MySQL CLI (`UPDATE users SET level = 9 WHERE id = 1`, for example) or your favorite database management interface (e.g. PHPMyAdmin), and start making some communities!
## Credits
Lead developers: [PF2M](https://github.com/PF2M), [EnergeticBark](https://github.com/EnergeticBark)

Developers: [Ben](https://gitlab.com/benatpearl), [Triangles](https://oasis.100percentnig.ga/users/triangles.py), [Chance](https://github.com/SRGNation) (previously), [jod](https://github.com/men-who-breathe) (previously)

Artwork: [Spicy](https://oasis.100percentnig.ga/users/mario7in1), [Inverse](https://oasis.100percentnig.ga/users/Inverse), [Gnarly](https://cvd-revived.gq/) (previously)

Marketing: [Pip](https://github.com/Pikacraft64)

Testing: [Mippy ❤️](https://indigo.cafe/users/Mario)
## Anything else?
Not much yet, thanks for asking.