# TODO

## Config

- [x] move docs.json into `./.autodoc` in root of project, add gitignore in this file which allows autodoc but will exclude the other files we'll add (indexing files, etc)
- [x] init command should create this folder and the docs.json file
- [ ] support recursive docs discovery so autodoc can find nested `docs/` folders across the repo, not only the root-level docs directory

## BM25 indexing
Provide a simple, LLM / CLI friendly way to do keyword search on our docs.

- [x] reserach using https://github.com/blugelabs/bluge for free text indexing of our docs
- [x] we will want a normalisation stage, read ./docs/indexing.md to see how other projects do it, design this too
- [x] write a ./docs/bm25-search.md doc describing how this will work
- [x] add new command group `search`. Add `reindex` which will take all our matching / included docs files, remove front matter, do any other processing (assume markdown) and then index the result
- [x] the `fixed` command should also reindex a file, as described above, overwriting its value in the index.
- [x] add a new `search keyword` cli command which will do a bm25 search based on index, output nice json results with score + path to doc + matching snippet. path is relative to project root.
- [x] add to main doc string which describes all of this in detail, including example keyword searches.
- [ ] is bm25 case insensitive? make this clear in the docs

## Docs

- [x] add a `quickstart` doc string command, outputs a thorough doc string describine all commands, roughly in order they are used, with examples for each, especially the search commands.
