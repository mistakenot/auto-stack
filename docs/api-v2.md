---
hash: "acff2972"
id: "5a2a90af"
read_when: "designing the unified auto binary API shape or planning cross-tool command consistency"
summary: "Long-term aspirational API shape for the auto-stack: unified binary, consistent command structure, and agent-first design principles."
title: "Auto API V2 Design"
---

# Api V2

What is the long term aspirational shape of our api?

## General principles

- Move away from seperate binaries (`autoetl`, `autosearch`) into a single unified binary. Why? Tools are becoming more dependant on eachother. Ease of installation.
- Have a consistent api shape across the different functions.
- Ensure that API shape is designed for agent usage first and foremost, instead of humans.

## API Shape

```
# ETL
auto 
  etl 
    run
auto
  search
    index
```
