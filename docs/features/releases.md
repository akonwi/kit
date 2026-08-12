# Release notes

Kit exposes release notes in a workspace panel opened from `/release-notes` or the update indicator in the header.

## Data sources

The installed version and its notes are bundled in `app/src/features/releases/current-release.ts`, so they remain available offline. Kit checks GitHub's latest-release endpoint at startup. The response body supplies the notes shown for an available update instead of reusing the installed version's bundled notes.

Release history is fetched lazily from GitHub when the panel is first opened. Kit fetches and renders three releases at a time; users explicitly load each additional page. Stable, published releases are included, while drafts and prereleases are omitted. Network and API failures are advisory and never interrupt startup. If history cannot be loaded, the installed release remains visible with its bundled notes.

## Presentation

The panel presents releases as one continuous, reverse-chronological stream. Each release is a section containing its version, publication date, status, and Markdown notes. Users scroll naturally through loaded releases without switching views, then activate **Load more releases** or press `m` to append another page of up to three GitHub release records. Clicking a release heading opens that release on GitHub; `o` opens the newest displayed release.
