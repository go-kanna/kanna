# Developing kanna

How to change the family across repositories: a change to go-kanna/internal and the generators using it, or a feature
spanning two generators and the showcase.

## Checking out

Check out each repository beside this one, as [ghq](https://github.com/x-motemen/ghq) lays them out:

```sh
for r in kanna go-kanna.github.io internal di fixture i18n mapper orm validate value showcase; do
	ghq get "github.com/go-kanna/$r"
done
```

## The family workspace

[`dev/go-kanna.work`](dev/go-kanna.work) is a Go workspace of every module of the checkouts beside this one, examples
and benchmarks included, so that each is built from the checkout rather than from the release its go.mod requires.
Point `GOWORK` at it to use it, in whichever way suits you; nothing here sets it.

Each repository's Makefile pins its own `go.work`, so that its targets run as its CI runs them, against the releases
it requires. To run one against the family workspace instead, override it:

```sh
make test GOWORK=$HOME/ghq/github.com/go-kanna/kanna/dev/go-kanna.work
```

## Checking a change

- Across the family: `dev/test-all.sh` runs `make test` in internal and each generator against the family workspace,
  and reports the ones that fail once all have run. Name repositories to run those alone:
  `dev/test-all.sh mapper validate`. The showcase's tests need its database and the URLs of `.env`, as its README
  says, so it runs only when named.
- On its own: `make test` in a repository, against the releases its go.mod requires, as its CI does.

A pull request to go-kanna/internal also runs each generator's tests against it, in its `generators` job.

## Translating

Each README has a `README.ja.md` beside it, and the landing page of go-kanna/go-kanna.github.io, `index.html`, has
`ja/index.html`. A change to one goes to the other in the same pull request. A translation leaves code blocks as they
are and keeps the headings, which `pages/check_translations.py` of that repository checks in its Pages workflow. Each of
its paragraphs is one line, since a line break in Japanese text renders as a space.

## Releasing

A change reaches users through tags, from the bottom up, each step a pull request merged after its CI passes:

1. Tag go-kanna/internal, `vX.Y.Z`, on its main.
2. In each generator using the change, require the new internal (`build: require internal vX.Y.Z`), and tag it.
3. In each generator, require its new tag in its examples and benchmarks (`build(example): require …`); `make example`
   regenerates them, and a generated file then names the release in its header.
4. Require the new releases in go-kanna/showcase, and regenerate it with `make gen`.

Until a tag exists, a repository can require another at a merged commit, by its pseudo-version, as
`go get github.com/go-kanna/internal@<commit>` does. A generated file names no version in its header then; requiring
the tag afterwards brings it back.
