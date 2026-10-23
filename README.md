# Spytify site

The GitHub Pages site of [spytify/spytify](https://github.com/spytify/spytify), built by GitHub Pages from this branch.

- The root holds the v1 documentation (the Windows recorder) and the donate page.
- `v2/` holds the v2 documentation. `v2/settings.md` and `v2/protocol.md` are generated from the code: in the main branch, run `go run ./internal/tools/gendocs -out <this checkout>/v2`.
- Each page takes its version from its folder (`defaults` in `_config.yml`); `_layouts/page.html` picks the menu (`_data/nav.yml`), the header and the version switch from it.

Preview with Ruby and Bundler: `bundle install`, then `bundle exec jekyll serve`, and open <http://localhost:4000/spytify/>.
