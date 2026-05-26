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
