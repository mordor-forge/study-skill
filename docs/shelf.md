# Shelf

Shelf is the Knowledge base plugin that ships with Lamplight: the `study-shelf` program,
installed beside `study`. It indexes a Topic's Sources on your own computer, PDFs and web
pages, and searches them, so that the agent can find the Passage a Lesson's Evidence quotes
from. It searches by keyword as it is installed, and by what a question means once you give
it an embedding model to ask, which you run yourself. It needs no account, and sends your
books nowhere but to the model you name.

`study` links none of it ([ADR-0012](adr/0012-knowledge-base-plugins-speak-one-contract.md)):
it starts `study-shelf` each time it has something to ask it, and speaks the
[Knowledge base plugin contract](knowledge-base-contract.md) with it, as it would with any
other plugin. Terms follow [GLOSSARY.md](../GLOSSARY.md).

## Registering it

`install.sh` and the deb and rpm packages install `study-shelf` next to `study`, and
`study setup` registers it on the computer, under the name your Topics will use, `shelf`:

```bash
study setup                        # registers the study-shelf beside study, among what it sets up
study knowledge-base check shelf
```

Setup takes the `study-shelf` beside `study`, and otherwise the first on your `PATH`. A
plugin you registered as `shelf` yourself is left as it is, and so you can register Shelf
by hand, from another place or with [`--allow-loopback`](#what-it-reads). Once setup has
registered Shelf, the name is taken, so say that yours replaces it:

```bash
study knowledge-base add shelf --replace -- /opt/lamplight/bin/study-shelf
```

The registry records which registration setup made. Yours is not one of them, so setup
leaves it as it is from then on, and `study setup --remove` does not remove it
([docs/cli.md](cli.md#shelf)).

`study doctor` says whether Shelf starts, whether the endpoints its settings name answer
([What study doctor tests](#what-study-doctor-tests)), and which Sources of your Topics it
has not indexed ([docs/cli.md](cli.md#study-doctor)).

`check` starts Shelf as `study` starts it for a Topic, and reports the contract it speaks:
version 1, Sources that are files and web pages, pages as locations, and how it searches:
`keyword`, and `embedding` too once [its settings file](#settings) names an embedding
endpoint. `study-shelf --version` prints its version, which is the version of the `study`
it was released with.

You never start `study-shelf` yourself. Started by hand it has no folder to keep its index
in, says so, and exits.

Then a Topic names it, and its Sources are indexed:

```bash
study topic update linear-algebra --knowledge-base shelf
study source index linear-algebra --dry-run   # which files and addresses Shelf would read
study source index linear-algebra
```

`study source list` and `study status` say afterwards what is indexed
([docs/cli.md](cli.md#what-is-indexed)).

## What it reads

| A Source that is | Shelf |
|---|---|
| A PDF with a text layer | Reads it page by page. Each Passage carries the pages it is on. |
| A PDF that is protected against changes and opens without a password | Reads it like any other. |
| A web page, by its `http` or `https` address | Fetches it once, when it is indexed, and takes the article's text: without the menus, the footer and the page's title. HTML and plain text are read. |

And what it does not read, with the reason `study` shows for the Source:

| A Source that is | Fails with |
|---|---|
| A scanned book, whose pages are pictures | `the file has no text layer`. Run it through a program that recognises the text (OCR), such as `ocrmypdf`, and add the file it makes. |
| A PDF that asks for a password to open | `the file is protected by a password`. Save a copy without the password and add that. |
| A PDF that is cut short or broken | `the file is damaged`. Download or export it again. |
| Any other file: EPUB, Word, Markdown, plain text, a web page saved under a book's name | `the file is not a PDF`. |
| A file that was moved or removed | `the file is not there`. |
| A file that changed since `study` read it, or while Shelf read it | `the file changed`. Index it again: a Source is indexed as the content `study` saw, or not at all. |
| A web address that sends a PDF, a picture or anything else that is no page | `Shelf reads web pages, which are HTML or plain text`. Download the file and add it as a file Source. |
| A page that shows nothing until a script has run, or that needs you to sign in | `the page has no text to read`, or the server's refusal. Shelf runs no script and has none of your browser's sessions. |
| A page served by your own computer: `localhost`, `127.0.0.1` | `… is this computer itself: Shelf fetches no page from it`. See below. |

Shelf reads a file and fetches an address only when it is asked to index it, which is what
`study source index` does, from your shell or your agent's. It fetches the address it is
given, from your computer, as your browser would: one on your own network too. A PDF is
recognised by how it begins, not by its name, so no other kind of file is read as one.

**Where Shelf fetches nothing from.** Your agent chooses which addresses are Sources, and
reads what was fetched back when it searches. Shelf fetches with your network, outside
whatever sandbox the agent is in. So two kinds of address are refused, for the page itself,
for every address it redirects to, and whatever name leads there:

- **Your own computer**: `localhost`, `127.0.0.1`, `::1`. Whatever listens there trusts
  whoever reaches it, since nobody else can. If you do serve the pages you study from to
  yourself, a book built with a local documentation server say, register Shelf to allow it:

  ```sh
  study knowledge-base add shelf --replace -- study-shelf --allow-loopback
  ```

  The registry is yours, outside the Study home, so nothing your agent writes turns this
  on.
- **The link-local addresses**, `169.254.0.0/16` and `fe80::/10`, always. No page to study
  from is served there, and a cloud's machines hand out their credentials at one of them.
  The three addresses where a cloud does the same outside that range are refused with
  them: `100.100.100.200`, `fd00:ec2::254` and `168.63.129.16`.

A proxy you set for Shelf (below) is yours to choose, wherever it listens, and it then
makes the connections: Shelf still refuses an address that is written as one of those
numbers, and cannot see where a name leads on the proxy's side. The proxy's own address
is one Shelf connects to only for a request that goes through it, never as a page.

Three more things to know about what comes out:

- **A Passage is the Source's own text**, about 1,500 characters of it, ending where a
  paragraph or a sentence does. Shelf writes no word of its own into one. It does tidy what
  a PDF holds: a ligature becomes its letters, characters that show nothing are dropped,
  and a word divided at the end of a line is put together again. Whether such a word is
  one word, `solu-tion`, or a pair that keeps its hyphen, `well-known`, the page does not
  say, so Shelf reads it from the book's own words: it is joined when the book holds the
  joined word elsewhere, keeps its hyphen when the book writes the pair with one or holds
  both halves as words, and is joined otherwise. A rare pair that the book writes once, at
  the end of a line, can so come out as one word.
- **A Passage keeps to one page**, always. A sentence that runs over the end of a page is
  in two Passages, one on each page: Evidence that quotes it names both, in order
  (`--passage` twice), and its location is then both pages. A printed book's running header, the chapter's title and the
  page number at the top of each page, is part of the page's text, and begins its first
  Passage.
- **Formulas come out as the PDF holds them**, which is often not as they look: a book
  typeset without a table of its symbols gives letters and odd characters where the page
  shows a sum. Quote the words around a formula, or record that Evidence without a Passage.

## Searching

Your agent searches with its `evidence_search` tool, and you can with
`study evidence search <topic> <query>`. What comes back are Passages: the book's own
text, each with its page. A quote the agent records as Evidence with the Passage it is
from is checked against that Passage, and refused when it is not what the book says
there. Quote a Passage as Shelf gives it, a joined word included: the check compares with
the Passage, not with the page. [docs/cli.md](cli.md#searching-a-topics-sources) has the
details.

Shelf searches in two ways, and puts what they find in one order.

**By keyword, always.** Shelf finds Passages by their words (SQLite's full-text index,
ranked by BM25). A query is words to look for, and any of them is enough: the Passages that
hold more of them, and rarer ones, come first. Words match without their case and their
accents, and by their English stem, so `eliminating` finds `elimination`. Nothing in a query
is an operator. A word is letters and numbers together, the small ones too, so `H₂O` and
`m²` are words and are found as they are written. **Text written without spaces between
words, as Chinese and Japanese are, cannot be searched by word**: a whole run of such text
is one word to the index.

**By what the question means, once [the settings file](#settings) names an embedding
endpoint.** When a Source is indexed, each of its Passages is sent to the endpoint, which
gives back a vector for it: a list of numbers that places the Passage among the others by
what it says. A search sends the question the same way, and finds the Passages whose
vectors are nearest the question's. So a question finds a Passage that answers it in other
words than its own, which no keyword search does.

**The two are put in one order**: two Passages of the embedding ranking, then one of the
keyword ranking, and so on, each Passage once. The Passage that means most like the
question comes first, and the one that best matches its words is among the first three,
whatever the model made of it. A search for a function's name, or a theorem's, so finds
the Passage that holds the name, where a model sees little in a name it never read.
[The measurements](#the-measurements) say why it is done this way and not the usual one.

**With a reranking endpoint**, the best thirty of that order, or as many as were asked for
when that is more, are handed to a second model with the question, and come back in the
order it gives them. That model reads the question beside each Passage, where an embedding
model reads each by itself: in the measurements it was the largest gain after the embeddings
themselves.

Searching reads the index and never a Source: a book that was moved away is still found,
and a search is answered while another book is being indexed. A search asks the embedding
endpoint once, for the question's vector, and the reranking endpoint once. Nothing else
Shelf does asks either of them, short of indexing and of the test `study doctor` asks for
([What study doctor tests](#what-study-doctor-tests)): not starting, and not saying what
is indexed.

### When an endpoint does not answer

A search is answered all the same, by what is left. An embedding endpoint that is down,
takes longer than 10 seconds, or answers with what is no vector leaves a search by keyword.
A reranking endpoint that fails, or takes longer than 20 seconds, leaves the order the
search had. Together that is 30 of the 45 seconds `study` gives a search, starting Shelf
included, so that Shelf is still there to answer and to say what it did without. Shelf says so in its answer, in three fields beside the Passages.
The `notice` is the contract's, and `study` shows it to you. The other two are Shelf's
own, which the contract lets a plugin add and `study` does not read:

| Field | Says |
|---|---|
| `search` | How these Passages were found: `["keyword"]`, or `["keyword", "embedding"]`. |
| `reranked` | Whether the reranking endpoint put them in their order. |
| `notice` | In words for you, what the search did less than its settings ask for, and why: `These Passages were found by keyword alone: the embedding endpoint 127.0.0.1:8080 could not be reached: …`. It is left out when there is nothing to say. |

Shelf names an endpoint by its host, and keeps neither the key nor the address in its
index. When an endpoint refuses, Shelf repeats the endpoint's own words, so that you can
see what is wrong, in a notice and in the reason of a Source that failed. `study` shows
both to you and hands both to your agent. Shelf takes three things out of those words
first:

- **The key in Shelf's settings**, whole, wherever it stands in them and whatever
  characters it holds.
- **Whatever follows the word "Bearer"**, in any letter case, after a space, a colon, an
  equals sign or a quotation mark: a gateway that sends a key of its own on repeats that
  one.
- **The address but for its host**: the address a request went to and the one the
  settings give, their paths, and every name of a path of twelve characters or more, as
  the settings give them and as they were sent, in any letter case.

Two things Shelf cannot tell from any other word, and repeats as the endpoint said them:

- a key that is not the one in Shelf's settings and does not stand right after the word
  "Bearer": in `invalid bearer token sk-…` it stands after `token`, and a gateway may
  name a key of its own anywhere in a sentence;
- a name of the address's path that is shorter than twelve characters, where a server
  repeats it by itself, as in `tenant acme has no model`.

So give nothing that must stay secret a short name in the address, and expect an endpoint
to keep its own keys out of what it says.

`study` shows you a `notice` wherever it shows a search. `study evidence search` prints it
under its first line, before the Passages, and gives it as `notice` with `--json`:

```
Knowledge base plugin shelf found 2 Passages for "elimination" in linear-algebra, best first:
! The plugin says of this search: These Passages were found by keyword alone: the embedding endpoint 127.0.0.1:8080 could not be reached: dial tcp 127.0.0.1:8080: connect: connection refused. Check that its server is running, and its address in Shelf's settings file.
```

Your agent's `evidence_search` tool returns it in the same field, and the skill has the
agent tell you what it says when it matters to what you asked, and not take the Passages
for the best your Sources hold. A notice is no error: the Passages are your Sources' own
text all the same, to learn from and to quote. When the endpoint answers again, so does
the search by it, and the notice is gone.

### Vectors of another model

A question's vector is compared only with vectors that were made the same way: by the model
the settings name now, of text marked as they say, with as many numbers. So when you give
Shelf an endpoint after a Topic was indexed, or name another model or another size in the
settings, what is indexed has no vectors a question can be compared with. It is still found
by keyword, and `study status` and `study source list` say of each such Source that it is
`outdated`, indexed and to be indexed again, with the reason:

```
its vectors are of the model nomic-embed-text, and Shelf's settings now name the model
google/embeddinggemma-2
```

`study source index <topic>` brings it up to date, and is what `study status` names as the
thing to do next. Shelf then gives the Passages that are there their new vectors, in one
step. It reads no file again and fetches no page for that, so the Passages and their ids
stay as they are. `study` hands Shelf a file only when it is on your computer and is the
one you added, so a book you have moved away since stays `outdated`, and found by keyword,
until it is back. Until every Source is done, a search is by vectors in the Sources that
have them and by keyword in the others, and its `notice` says how many those are.

Shelf tells the model by the name in its settings. A server that comes to serve another
model under a name it served before is one Shelf cannot tell from the first: remove the
Topic's file from Shelf's folder then (see below), and index again.

## What study doctor tests

Shelf starts, says which contract it speaks and says what is indexed whether or not the
endpoints in its settings file answer: none of that asks them. So `study doctor` and
`study knowledge-base check shelf` have Shelf test them, and you hear that an endpoint is
down before a book is read for nothing, or a search has quietly fallen back to keyword.

- **The embedding endpoint is asked for the vector of one question, as a search asks
  it**: the same address and request, with the Recipe's marking of a question or the
  fields it adds to the request, `dimensions` when the Recipe sends it, and the key. The
  answer is held to what a search holds it to: one vector, of at least the `dimensions`
  your settings name, that is not all zeros and holds nothing but numbers.
- **The reranking endpoint is asked to order two short texts for one question**, and its
  answer is held to its shape: a place and a score for each text.
- **What is sent is Shelf's own, the same every time**: the question "What does a shelf
  hold?", and the texts "A shelf holds books." and "A lamp gives light." Nothing of a
  Topic, a Source or a question of yours goes to an endpoint for a test.
- **Each endpoint has 8 seconds, and the two are asked side by side.** `study` gives a
  plugin 10 seconds to say what it found, so Shelf says in time which endpoint did not
  answer. A search gives the embedding endpoint 10 seconds, and the reranking endpoint
  20: a server that needs longer than 8 for its first answer, since it loads the model
  then, is a warning once and none when you ask again.
- **With no endpoint in the settings file there is nothing to test**, and the doctor
  says nothing of Shelf's endpoints.
- **A finding names an endpoint by its host**, like everything Shelf says of one: never
  its key, nor the rest of its address, whatever the endpoint itself answered.
- **A finding that is all right says which model Shelf asked for.** When the endpoint
  names the model it answered with otherwise, the finding says that name too, and is all
  right all the same: a server names one model by its file, with a tag or in another
  case, and one that serves a single model answers with it whatever it is asked for.
  Shelf cannot tell these apart. If the name is of a model you did not mean to serve,
  the server is the one to look at.

`study doctor` shows a Finding for each endpoint, `knowledge_base:shelf:embedding` and
then `knowledge_base:shelf:reranking`. One that is not all right is a warning, and never
keeps you from studying. It says what to look at, where Shelf can tell, and ends with
what Shelf does meanwhile: without the embedding endpoint no Source is indexed, and a
search is by keyword alone; without the reranking endpoint a search gives its Passages in
the order it found them. While the embedding endpoint is not all right, the doctor's
Finding of each Topic that names Shelf says so beside `study source index`: that command
may fail until the endpoint answers.

| The warning says | What to do |
|---|---|
| `could not be reached` | The server is not running, or `endpoint` is not where it listens. Start it, or correct the address: for a server that speaks the OpenAI API it ends in `/v1`. |
| `did not answer within 8s` | The server is busy, or loading its model. Ask again once it is up. |
| `answered` a number from 500 to 599 | The server failed on its own side: its own log says why. |
| `answered 401 Unauthorized`, `answered 403 Forbidden` | The `key` in the settings file, or in the file `key_file` names, is not one the server takes. |
| `answered 404 Not Found` | The address is not the API's, or the server does not know the `model`. Check `endpoint` and `model`, or the Recipe. |
| `answered` a number from 300 to 399 | Shelf follows no redirect. Give the address that answers. |
| `gave vectors of 384 numbers, and Shelf's settings ask for 512` | The server has another model than the Recipe is for, or `dimensions` is more than the model gives. Serve the model the Recipe names, or set `dimensions` to a size the model has. |
| `gave a vector that is all zeros, or holds what is no number` | The server answers and its model does not: look at the server's own log. |
| `did not answer as its API does`, `ranked 1 Passages of the 2 it was handed` | What answers at that address is not the service the settings take it for. Check `endpoint`, and that it is the embedding server's in `[embedding]` and the reranker's in `[reranking]`. |

Once it is put right, `study doctor` says so. A Source whose indexing failed meanwhile is
tried again by `study source index <topic>`, and Sources that were indexed with another
model are said to be [outdated](#vectors-of-another-model), with the same command.

## Where the index is

In your state folder, outside the Study home, one file for each Topic:

```
~/.local/state/lamplight/knowledge-base-plugins/shelf/<collection>.sqlite
```

(`$XDG_STATE_HOME` instead of `~/.local/state` when it is set; `shelf` is the name you
registered it under, and the collection is the name `study` gives the Topic's Sources when
it speaks to a plugin: it begins with the Topic's id.) Beside it are `<collection>.lock`,
which says which Sources are being indexed now, and, while a Shelf runs, two files of
SQLite's, ending in `-wal` and `-shm`.

- It is that computer's state. It is not synced with the Topic, and it is rebuilt by
  indexing the Topic's Sources again. To have a Topic indexed from nothing, remove its
  files.
- Indexing a Source whose content is indexed already does nothing, unless its Passages
  lack the vectors Shelf's settings now ask for (above). Content that changed replaces what
  was indexed, in one step: a search finds the old text until the new text is all there,
  and never half of either. Stopping an indexing, or losing power in the middle of one,
  leaves what was indexed before, and the Source is indexed when you ask again.
- With an embedding endpoint, a Source's vectors are in the same file, written in the same
  step as its Passages: a Source is indexed with a vector for each of its Passages, or not
  at all. An endpoint that fails while a Source is indexed leaves the Source `failed`, with
  the reason, and what was indexed of it before. An indexing that was stopped starts over,
  and asks for every vector again.
- `study` starts Shelf for each thing it asks and stops it again, so several run at once:
  a `study status` in one terminal while `study source index` works in another. Each
  finds what another indexed, and says of a Source another is indexing that it is being
  indexed, and how far: as soon as the other was asked for it, while it waits for its
  turn too. When a third is writing a book into the same collection at that moment, it
  says so once that writing is over. Why a Source failed is kept in the collection, so
  it is still said the next day.
- Each file carries a format number. A file written by a newer Shelf is refused, with the
  reason, and left as it is. The format is 2 since Shelf keeps vectors. A file in format 1,
  which a Shelf from before embedding search wrote, is read as it is, and becomes one in
  format 2 the next time a Source is indexed into it: nothing is to do for it.
- **A sandbox that `study source index` runs in must allow that folder**, since Shelf is
  then started inside the sandbox
  ([the contract](knowledge-base-contract.md#the-plugins-folder)). Or index from a shell of
  your own. Where Shelf may read the folder and not write in it, what is indexed is still
  said and still found, and indexing fails with that as its reason. With an embedding
  endpoint, the sandbox must let Shelf reach that too, or every Source fails for it.

Shelf opens that folder once and follows no link out of it. It writes nowhere else: not in
the folder it is started in, and not in a temporary folder.

## What it costs

Asking what is indexed is quick and small: Shelf starts, answers and is gone in a few
hundredths of a second, whatever its settings. Indexing a PDF is the work:

- The PDF reader is compiled for your computer each time `study source index` has a PDF to
  index, which takes about three seconds.
- A book of a thousand pages then takes ten to fifteen seconds, and one with a drawing on
  every page a minute or more. Shelf holds a few hundred megabytes while it reads.
- One file may take 15 minutes and 64 MB of text, and the PDF reader 1 GB of memory. A
  file that needs more fails with the reason. A web page may be 16 MB, take 60 seconds, and
  redirect five times.
- A Topic's collection holds 10,000 Sources, as many as `study` can be told the state of. A
  further one is refused with that as the reason.

With an embedding endpoint, most of the time of indexing is the model's:

- A Source's Passages are sent sixteen at a time, one request after the other, and each
  request has two minutes. The four books of [the measurements](#the-measurements), 5,062
  Passages, were embedded in 69 seconds by a graphics card, 74 Passages a second. On a
  processor alone, with 8 threads, the same model made 8.6 Passages a second: ten minutes
  for the four, two and a half for a book of 1,300 Passages.
- A vector of 512 numbers is 2 KB in the Topic's file: 10 MB for those four books.
- A search asks the endpoint for one vector, which took 90 ms on that processor, and then
  reads every vector of the Topic and compares it with the question's. `study` starts
  Shelf anew for every search, so nothing is kept between two of them. That reading and
  comparing, from a Shelf that was just started, took:

  | Passages in the Topic | 512 numbers | 768 numbers |
  |---|---|---|
  | 5,000, about four books | 10 ms | 12 ms |
  | 50,000, about forty | 122 ms | 158 ms |

  on one core of a desktop processor (a Ryzen 9 3900X) with the file in memory. So Shelf
  keeps no index of the vectors and needs no vector database: at the size a Topic has, a
  few thousand Passages, a plain reading costs a fraction of the question's own vector.
- Reranking thirty Passages took about a second a question on a graphics card.

## Settings

Shelf's settings are a file of your own:

```
~/.config/lamplight/shelf.toml
```

(`$XDG_CONFIG_HOME/lamplight/shelf.toml` when that variable is an absolute path.) It is
beside Lamplight's `config.toml`, outside the Study home, so nothing your agent writes
decides where Shelf sends a book's text. Without the file Shelf searches by keyword, as it
is installed.

```toml
format = 1

[embedding]
endpoint = "http://127.0.0.1:8080/v1"
recipe = "embeddinggemma-2"

[reranking]
endpoint = "http://127.0.0.1:8081/v1"
model = "qwen3-reranker-0.6b"
```

**Nothing is a setting on Shelf's command line or in its environment**, but
`--allow-loopback` above. A command line shows in the process list and in
`study knowledge-base list`, and `study` starts a plugin with a few variables of its own
making: an `OPENAI_API_KEY` or `OPENAI_BASE_URL` in your shell never reaches Shelf. Its
environment tells Shelf where your configuration folder is, and nothing else.

After a change, `study knowledge-base check shelf` says whether Shelf can use the file
and how it searches now, and has Shelf test each endpoint the file names, as
`study doctor` does: one request to each, of Shelf's own text
([What study doctor tests](#what-study-doctor-tests)).

| Setting | Is |
|---|---|
| `format` | `1`. The file says which format it is in, and one in a newer format is refused. |
| `[embedding]` | The endpoint that gives Shelf its vectors. Leave the table out to search by keyword alone. |
| `endpoint` | The address the endpoint serves the OpenAI API at, as an OpenAI client's base address has it: `http://127.0.0.1:8080/v1` for llama.cpp, `http://127.0.0.1:11434/v1` for Ollama. Shelf posts to `/embeddings` below it. An address without a path stands for `/v1`. `http` or `https`, with no user name, password or query in it. |
| `recipe` | A [Recipe](#recipes)'s name: the settings below, for one model. A setting you give beside it takes the place of the Recipe's. |
| `key` | The endpoint's key, when it takes one: one word, and spaces around it are not part of it. It is sent as the request's bearer token, and to that endpoint alone: Shelf follows no redirect. |
| `key_file` | Instead of `key`: a file that holds the key and nothing else ([A key in a file of its own](#a-key-in-a-file-of-its-own)). |
| `model` | The model's name, as the endpoint knows it. Required without a `recipe`. |
| `dimensions` | How many numbers Shelf keeps of each vector. A longer vector is cut to its first numbers and made of length 1 again, which only a model trained for it allows. A shorter one fails the Source. Always said, by you or by the Recipe: the size the model gives, which its own page says (768 for many), or fewer where the model was trained for that. Shelf then knows what it keeps, and sees it in the settings when the size changes. |
| `send_dimensions` | `true` has the request ask the endpoint for vectors of that size, as the OpenAI API's `dimensions` does. Shelf checks the size either way. |
| `query`, `document` | Text for Shelf to write around a question and around a Passage, for a model that wants them marked: `query = "search_query: {text}"`. `{text}` is where the text goes, and `{title}`, in `document`, the Source's title. |
| `query_request`, `document_request` | Fields for Shelf to add to the request instead, for a server that marks the texts itself: `query_request = { prompt = "query" }`. Text, numbers and `true` or `false`. |
| `passage_size` | About how long a Passage is, in characters, from 200 to 5000: no longer than the model reads at once. It decides how Sources are cut from then on, and changes nothing that is indexed. |
| `batch` | How many Passages one request holds while a Source is indexed, from 1 to 256. 16 unless you or the Recipe say. A server that takes fewer refuses the request, and the Source fails with what the server said. |
| `[reranking]` | The endpoint that orders a search's Passages. Leave the table out for none. It works without `[embedding]` too, on what keyword search found. |
| `endpoint` | As above. Shelf posts to `/rerank` below it, as llama.cpp's server takes it: the `model`, a `query`, the `documents` and `top_n`, answered with an `index` and a `relevance_score` for each document. |
| `key`, `key_file`, `model` | As above. `model` is required. |
| `candidates` | How many of a search's best Passages are handed over, from 2 to 100. 30 unless you say, and never fewer than a search asks for. |

**A request holds what the settings name, and nothing else**: the model, the texts, the
size when `send_dimensions` says so, and the fields of `query_request` or
`document_request`. Some servers refuse a request for one key they do not know.

**A question and a Passage are marked in one way, never both**: by `query` and `document`,
which Shelf writes, or by `query_request` and `document_request`, which have the server
write. A server that marks a text Shelf had marked would mark it twice, so a file that
gives both is refused.

**A file Shelf cannot use stops it.** Shelf then does not start, and
`study knowledge-base check shelf` shows why, as `study doctor` does: a line that is no
TOML, a setting or a Recipe it does not know, a newer format, an address that is none.
Shelf does not search by keyword instead: you would think the endpoint you wrote in use.
It says which line or setting is wrong and never what it holds, since it may hold the
key. Keep the file yours alone to read (`chmod 600`) when it has a key in it.

### A key in a file of its own

A key in `shelf.toml` makes the settings file a secret: it cannot be kept with your other
settings, and a program that keeps your secrets would have to write all of it. So a table
can name a file for its key in place of the key:

```toml
[embedding]
endpoint = "https://embeddings.example/v1"
recipe = "embeddinggemma-2"
key_file = "embedding.key"
```

- **The file holds the key and nothing else**: one word, with or without a line's end
  behind it, as `printf` or a secrets manager writes it. Keep it yours alone to read
  (`chmod 600`).
- **It is named, and is beside `shelf.toml`**, in Lamplight's configuration folder. A
  path is refused, whole or not: what the file holds is sent to the endpoint, so settings
  that could name any file could have any file of yours sent. When you keep the key
  elsewhere, link it there: `ln -s ~/.secrets/embedding.key ~/.config/lamplight/`.
- **`key` and `key_file` in one table is refused**: one of the two.
- **It is not an environment variable**, and cannot be: `study` starts Shelf with a few
  variables of its own making, and a key in your shell never reaches it.
- **The file is held to what the settings file is held to**: a file of its own, of 4096
  bytes at most, with no second name elsewhere, and nowhere in the folders an index is
  kept in. One Shelf cannot use stops it, with the file's name in the reason and never
  what it holds.
- **Keep it outside your Study home**, like the settings file. `study doctor` warns of a
  `shelf.toml` that leads there, and reads nothing of it, so it does not see where a key
  file is.

**The file is not read from where an index is kept.** A settings file inside the folder
Shelf keeps its index in, or inside the folder above it that holds every plugin's folder
(`knowledge-base-plugins` in your state folder), or reached through either by a link, is
refused. Those folders hold data, and you may have let your agent's sandbox write there so
that `study source index` can run inside it: a settings file found there would be the
agent's to write, and with it the address Shelf sends your books to, and its key, from
outside the sandbox. A file with a second name elsewhere (a hard link) is refused for the
same reason, wherever the other name is. **Do not link the file into your Study home
either**, to have it sync with your notes: your agent writes there, and Shelf cannot know
where your Study home is to refuse it. `study` does know, so `study doctor` warns when the
file is in the Study home or is reached through it, as `shelf_settings`. A link to a file
of your own elsewhere, where you keep your other settings, is fine.

**Behind a proxy**, Shelf needs to be told, to fetch web pages and to reach an endpoint
that is not on your computer. `study` starts a plugin with a small environment of its own
making, in which no proxy variable arrives. Register a script of your own, kept outside
the Study home, that sets it:

```sh
#!/bin/sh
export HTTPS_PROXY=http://proxy.example:3128
exec study-shelf "$@"
```

An endpoint on your own computer is never reached through the proxy, and neither is one
whose address is `http`, wherever it is: a proxy that passes an `http` request on reads
all of it, the key and every Passage. Through a proxy, give the endpoint an `https`
address. And the rule that Shelf fetches no web page from your own computer is for
Sources, which your agent names: an endpoint is yours to name, and is usually there.

### Recipes

A Recipe is a named set of these settings for one embedding model. Shelf has two, both for
EmbeddingGemma 2, Google's open model (Apache 2.0), which differ in who marks the texts:

| Recipe | For | It sets |
|---|---|---|
| `embeddinggemma-2` | A server that embeds a text as it is sent, such as llama.cpp, Ollama and vLLM. Shelf writes the model's marks itself, and cuts the vectors. | `model = "google/embeddinggemma-2"`, `dimensions = 512`, `query = "task: search result \| query: {text}"`, `document = "title: {title} \| text: {text}"`, `passage_size = 1500` |
| `embeddinggemma-2-prompts` | A server that applies the model's prompts itself, by name, and refuses a key it does not know. Shelf sends the texts as they are. | `model = "google/embeddinggemma-2"`, `dimensions = 512`, `send_dimensions = true`, `query_request = { prompt = "query" }`, `document_request = { prompt = "document" }`, `passage_size = 1500`, `batch = 8` |

- **Set `model` beside the Recipe when your server knows the model by another name.** Ollama
  wants the name you pulled it under.
- **512 numbers of the model's 768.** The model is trained so that the first numbers of a
  vector do nearly the work of all of them. On the four books of the measurements the right
  Passage was found as often with 512 as with 768, to within a question in a hundred, and a
  third less is kept, read and compared for every search. `dimensions = 768` keeps them
  all; 256 lost a little, and 128 a lot.
- **Passages of about 1,500 characters**, the size the measurements were made with, which
  is Shelf's own without a Recipe too. The longest Passage Shelf then cuts is 2,001
  characters, which the model reads whole when they are prose. A page of symbols or of
  code is more tokens for as many characters: when your server refuses a Passage for its
  length, the Source fails with what the server said, and a smaller `passage_size` or a
  server started to take more is what helps.
- **`embeddinggemma-2-prompts` sends no title.** The server writes the model's prompt for
  a document itself, which has no title to put in it; `embeddinggemma-2` writes the
  Source's title before each Passage, as the measurements were made. What the title is
  worth was not measured.
- **A new title embeds nothing again.** With `embeddinggemma-2` the title a Source had when
  it was indexed is in what the model read of each Passage. Give the Source another title
  later, and its vectors stay the ones made with the first: a title is no content, and a
  Source is not indexed again for one. To have the new title in them, remove the Topic's
  file from Shelf's folder (see below) and index again.
- **Eight Passages in a request, with `embeddinggemma-2-prompts`.** A server of that kind
  is often the model's own library behind a small program, and takes few texts at once:
  the one this Recipe was held to refuses more than eight. Set `batch` when yours takes
  more.
- Changing from one Recipe to the other is a change of how Passages were marked: Shelf
  cannot know that the two servers mark alike, so it says of what is indexed that it is to
  be indexed again.

**Any other model** works through settings of its own, without a `recipe`: its `model`,
its `dimensions`, and what it needs of the rest. One that wants no mark needs those two
alone. One that wants a question and a document marked says how:

```toml
format = 1

[embedding]
endpoint = "http://127.0.0.1:11434/v1"
model = "nomic-embed-text"
dimensions = 768
query = "search_query: {text}"
document = "search_document: {text}"
```

## Serving EmbeddingGemma 2

Shelf downloads no model and starts no server: you run one, and name its address. The
`llama-server` lines below were run as they are written, on a graphics card, with a build
of llama.cpp of 8 October 2026 (commit `de7fa0a`). Flags change between versions, so ask
your server's own `--help` when one is refused. The Ollama address is from Ollama's
documentation and was not tried.

**llama.cpp** serves the model's text side through `llama-server`, from a GGUF file of the
model. The measurements were made with it, and a 4-bit file, of 176 MB, gave the same
results as an 8-bit one. In October 2026 the model needed a recent build of llama.cpp.

```sh
# On a processor alone, with 8 threads.
llama-server -m embeddinggemma-2.gguf --embedding --host 127.0.0.1 --port 8080 \
    -c 8192 -b 2048 -ub 2048 -t 8

# With a graphics card: every layer on it.
llama-server -m embeddinggemma-2.gguf --embedding --host 127.0.0.1 --port 8080 \
    -c 8192 -b 2048 -ub 2048 -ngl 99
```

The sizes are what matter: a server refuses a text that is longer, in tokens, than what it
was started to take at once (`-ub`, and the context `-c`), with "input is too large to
process. increase the physical batch size". A Passage of 2,000 characters is several
hundred tokens, and sixteen of them go in one request as these lines start the server.
The model's file says how its vectors are pooled, so `--pooling` is not needed. The
settings for it are `endpoint = "http://127.0.0.1:8080/v1"` and
`recipe = "embeddinggemma-2"`.

**Ollama** serves the OpenAI API at `http://127.0.0.1:11434/v1`, and uses a graphics card
when it finds one. Use the Recipe `embeddinggemma-2`, and set `model` to the name
`ollama list` shows for the model.

**A server that applies the model's prompts itself** is one whose requests name a prompt
of the model's, `"prompt": "query"` or `"prompt": "document"`, and which writes that
prompt's text before the input. Use the Recipe `embeddinggemma-2-prompts` for it: with the
other, every text would be marked twice. Such a server may also refuse a request for a key
it does not know, which is why Shelf sends none but what its settings name.

**A reranker**, with llama.cpp, is a second `llama-server` on another port, started with
`--reranking` and a reranking model's GGUF file. The measurements used
Qwen3-Reranker-0.6B.

```sh
llama-server -m qwen3-reranker-0.6b.gguf --reranking --host 127.0.0.1 --port 8081 -ngl 99
```

To hold your endpoint to a Recipe, Lamplight's own test asks it for vectors and checks what
comes back: that it takes the Recipe's requests, a whole batch of the longest Passages at
once among them, that the vectors have the Recipe's size, and that eight questions each
find the Passage that answers them. It does not tell you that you chose the wrong one of
the two Recipes: a server that marks the texts itself also takes texts Shelf has marked,
and still finds the eight. Both Recipes were held to a real server this way:
`embeddinggemma-2` to llama.cpp with the 4-bit file, and `embeddinggemma-2-prompts` to
the model's own library on a graphics card. From a checkout of the repository:

```sh
LAMPLIGHT_TEST_EMBEDDING_ENDPOINT=http://127.0.0.1:8080/v1 \
    go test ./internal/shelf -run TestTheRecipeAgainstARealEndpoint -v
```

`LAMPLIGHT_TEST_EMBEDDING_RECIPE` names the Recipe when it is not `embeddinggemma-2`,
`LAMPLIGHT_TEST_EMBEDDING_MODEL` the model when your server has another name for it,
`LAMPLIGHT_TEST_EMBEDDING_KEY` a key, and `LAMPLIGHT_TEST_RERANKING_ENDPOINT` with
`LAMPLIGHT_TEST_RERANKING_MODEL` a reranker to try with it. These variables are the
test's: Shelf itself reads none.

## The measurements

Made on 9 October 2026 on four of the maintainer's books, a programming classic, a book on
data systems, an algorithms textbook and a linear algebra textbook: 5,062 Passages of about
1,500 characters, and 300 questions that a language model wrote, each from one Passage and
in other words than the Passage's. A search is right when it finds that Passage.
EmbeddingGemma 2 was served by llama.cpp, with the marks of the Recipe `embeddinggemma-2`
and vectors of 768 numbers, and the reranker was Qwen3-Reranker-0.6B.

| Search | The right Passage first | In the first 5 | In the first 20 |
|---|---|---|---|
| Keyword | 32% | 64% | 87% |
| Embeddings | 51% | 84% | 95% |
| Keyword and embeddings, then the reranker over the best 30 | 64% | 94% | 97% |

A point is three questions, so a difference of a point or two is what chance gives. The
linear algebra textbook was the hardest for every way of searching. A context written by a
language model for each Passage, which Shelf does not do, gained a point or two for a
hundred times the time of indexing.

The rest was worked out from the vectors of that experiment while embedding search was
built, without asking a model again.

**How many numbers of a vector to keep:**

| Numbers | First | In the first 5 | In the first 10 | In the first 20 |
|---|---|---|---|---|
| 768 | 51.0% | 84.0% | 91.3% | 95.0% |
| 512, the Recipe's | 50.3% | 83.3% | 91.7% | 94.0% |
| 256 | 47.0% | 81.7% | 89.3% | 93.3% |
| 128 | 40.3% | 72.0% | 80.7% | 90.0% |

**How to put the two rankings in one order**, with vectors of 512 numbers and the best 50
of each ranking. The last column is where a Passage would stand that keyword search puts
first and the embeddings do not find at all, as one that holds a name the question spells
may be: for the middle one of the 300 questions, and for the worst. None of the questions
asks for a name, so that is worked out and not measured.

| Order | First | First 5 | First 10 | First 20 | First 30 | A name only keyword finds |
|---|---|---|---|---|---|---|
| Embeddings alone | 50.3% | 83.3% | 91.7% | 94.0% | 95.7% | Not found |
| Reciprocal rank fusion, with its usual constant of 60 | 50.3% | 81.7% | 87.7% | 94.3% | 96.7% | Place 20, at worst 40 |
| Reciprocal rank fusion, with a constant of 5 | 51.3% | 85.3% | 92.3% | 96.3% | 96.7% | Place 5, at worst 9 |
| One of the embeddings', then one of keyword's | 50.3% | 81.3% | 91.3% | 96.0% | 97.0% | Place 2 |
| **Two of the embeddings', then one of keyword's** | 50.3% | 84.0% | 92.3% | 95.3% | 97.0% | Place 3 |
| Three, then one | 50.3% | 84.3% | 91.7% | 95.3% | 97.0% | Place 4 |

Reciprocal rank fusion scores a Passage by its place in each ranking and adds the scores
up. With its usual constant, every Passage that is in both rankings, about twenty of fifty,
comes before one that is first in one of them alone: the name is lost, and so are four
questions in a hundred among the first ten. Taking two for one finds as much as the
embeddings alone at every length of list, a little more further down, and has the keyword
ranking's best in a fixed place near the top. With a smaller constant the fusion found as
much, without that promise.

The reranker was first measured over the best 30 of reciprocal rank fusion, and on
13 October 2026 again over the best 30 of Shelf's own order, both with vectors of 512
numbers:

| The reranker over the best 30 of | First | First 5 | First 10 | First 20 |
|---|---|---|---|---|
| Reciprocal rank fusion, constant 60 | 64.3% | 94.0% | 96.0% | 96.7% |
| **Two of the embeddings', then one of keyword's** | 64.7% | 93.7% | 96.0% | 97.0% |

The reranker makes the same of either: what differs is what a search gives without one.

The books are the maintainer's, so neither they, nor the questions, nor the vectors are in
the repository: these numbers can be made again with other books, and not checked against
these.

## What is inside

Shelf is one program without dependencies to install. It is built from:

| Part | Is | Licence |
|---|---|---|
| The index | SQLite, through `modernc.org/sqlite`, a translation of SQLite to Go | Public domain; BSD-3-Clause |
| The PDF reader | PDFium, the reader Chrome uses, compiled to WebAssembly and packaged by `github.com/klippa-app/go-pdfium` | BSD-3-Clause and Apache-2.0; MIT |
| What runs the PDF reader | `github.com/tetratelabs/wazero`, a WebAssembly runtime | Apache-2.0 |
| The text of a web page | `codeberg.org/readeck/go-readability`, after the reader view of Firefox | MIT |
| The settings file | `github.com/BurntSushi/toml`, which `study` reads its own files with | MIT |

No model is inside, and nothing that runs one: the vectors are compared by Shelf's own few
lines, and the models are served by what you run.

The licence of each, and of everything else `study-shelf` is built from, comes with every
release, in its `NOTICES` folder. `NOTICES/pdfium` there says what the PDF reader itself is
made of.

The PDF reader runs inside Shelf, shut in: it is given the one file to read, and can open
no other file, reach no network, and take no more memory than it is allowed. Why PDFium was
chosen over `pdftotext` is in
[ADR-0012](adr/0012-knowledge-base-plugins-speak-one-contract.md).
