# Blind-reader questions

You are reading three short programs in a language you have not seen: `dashboard.ef`, `concurrency.ef` and `layers.ef`. Some packets also contain a `library.ef`; read it only if the programs call something you cannot work out. Answer from the code alone, in a sentence or two each. If you are unsure, say so and give your best guess. Do not look anything up.

Record how long the whole packet took you, and for each answer how confident you are (sure / fairly sure / guessing).

## dashboard.ef

1. Does the 2-second limit in `main` include the time it takes to set up `App`?
2. What does `main` return if `dashboard` takes longer than 2 seconds?
3. If the invoice lookup takes longer than 500 ms, what happens to the rest of `dashboard`?
4. When the 2-second limit is hit, what happens to the account lookup that `dashboard` started in the background? Does it keep running after `main` returns?
5. Which services does a caller have to supply before it can run `dashboard`?

## concurrency.ef

6. What does `program` print, in order?
7. The second background task (`stopped`) sleeps for 10 seconds. Does `program` wait for that sleep? Why or why not?
8. `bad()` always fails. Does `program` fail because of it? Why or why not?

## layers.ef

9. What does `main` return?
10. When `main` runs, which `Store` does `AccountLive` read from?

## All files

11. Which words or forms in these files are built into the language (the compiler gives them special meaning), and which are ordinary functions or methods someone could have written in a library? List the ones you notice.
12. Suppose another team wants to run `dashboard` with a 5-second limit instead of 2. Can they do that without editing `dashboard`? Show the line they would write.
