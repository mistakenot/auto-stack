---
hash: "b33a9747"
id: "2750b106"
summary: "Requirements for autoweb, a safe web research portal for AI coding agents with pluggable backends and result deduplication."
title: "Autoweb Requirements"
---

Autoweb is a tool to make safe and efficient web research easier for AI coding agents. Rather than using curl or native webfetch to download files, Autoweb gives us a single portal to access the internet to help make locked down coding agents safer and to provide help for utilities to make searching and filtering the results easier.

## Features

- Pluggable deep research backends with a single interface (exa / parallel / openai / etc)
- Kick off multiple providers at once, condense all results into one result, pick the best.
- Learn from successful web research requests to help turn future requests.
- Maintains history of searched / useful pages (?)
- Convert web pages to markdown