# Recent research

Research keeps discoveries and text material in local research records. Saving excerpts, saving a report, or importing the full source are explicit actions that create knowledge items.

Quick mode runs one query and one page (20 results) per platform. Deep mode plans once, preserves the original question, and shares three pages (60 results) across at most three queries per platform. It reads up to six sources, with a 180-second item deadline and a ten-minute batch deadline. Reports are always generated manually.

Open **Materials and references** to inspect original text and the frozen report evidence. Reports use up to 12,000 characters in quick mode or 32,000 in deep mode. Stored material has a separate 200,000-character limit. Truncation and source limitations are shown explicitly.

Xiaohongshu and Douyin expose authenticated text or descriptions and up to 20 comments. A video description is not a transcript. Bilibili uses an already installed yt-dlp for metadata and publisher subtitles, with automatic subtitles as a fallback. Research does not download media, run transcription/OCR/image generation, install dependencies, or import material automatically.

Evidence requires relevance of at least 30/100, entity matching where applicable, and a confirmed date inside the frozen window. Limits are three items per author per platform, ten per platform, thirty overall. Up to three uncertain-date leads can appear only in limitations or verification leads. Empty recent evidence prevents a report model call. Valid citation IDs do not establish factual correctness.

**Research again** preserves the sequence and plan; **Replan and research** creates a new plan. Independently created questions are separate sequences. **Changes** compares against an eligible earlier result without calling a model. New discoveries do not mean new publications; missing search results do not mean deletion. Failed sources, sampling caps, and plan changes make missing-result trends indeterminate.

**Link existing knowledge** is off by default and requires selecting a collection or all knowledge. Existing FTS and embeddings are reused without rebuilding an index. Scope is enforced before ranking and checked again after retrieval. Archived items are included; trashed items and saved reports from the same sequence are excluded. Up to six local excerpts of 1,000 characters use separate `L` references as historical background.

Saved reports include the evidence excerpts actually cited and remain readable after deleting the research record. Existing knowledge and Wiki content are not changed automatically. Interrupted work remains local; restarting the app does not automatically resume network or model calls.

Migrations 0025–0027 preserve old research content. Desktop startup creates an existing-format pre-update backup before the schema upgrade. Rollback requires a compatible pre-upgrade backup. Release, push, production database migration, recurring research, notifications, and additional platforms are outside this change.

See [the Chinese guide](./research.md) for the detailed policy and [validation record](./research-validation.md) for actual checks. Isolated tests and screenshots are separate from live platform and model validation.
