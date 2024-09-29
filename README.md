zstd-pbf is a tool for re-compressing data in OSM's PBF files from
either uncompressed or zlib compression to zstd compression.

Note that zstd compression is an optional feature of PBF files and
some software may not support it
(see [wiki.openstreetmap.org/wiki/PBF_Format](https://wiki.openstreetmap.org/wiki/PBF_Format)).

# Installation
```console
$ go install github.com/codesoap/zstd-pbf@latest
```

# Usage
```console
$ zstd-pbf -h
Usage:
  zstd-pbf [-fastest|-better|-best] <IN_FILE> <OUT_FILE>
Options:
  -best
        use the compression level with the best compression
  -better
        use a compression level with better compression than default
  -fastest
        use the fastest compression level
```

# Example
```console
$ wget 'https://download.geofabrik.de/europe/germany/bremen-latest.osm.pbf'

$ # First, let's try the default compression:
$ zstd-pbf bremen-latest.osm.pbf bremen-latest.zstd.osm.pbf
$ du -ah bremen-latest.*
19.3M   bremen-latest.osm.pbf
21.2M   bremen-latest.zstd.osm.pbf

$ # What about faster compression?
$ rm bremen-latest.zstd.osm.pbf
$ zstd-pbf -fastest bremen-latest.osm.pbf bremen-latest.zstd.osm.pbf
$ du -ah bremen-latest.*
19.3M   bremen-latest.osm.pbf
21.2M   bremen-latest.zstd.osm.pbf

$ # Let's see the best zstd can do:
$ rm bremen-latest.zstd.osm.pbf
$ zstd-pbf -best bremen-latest.osm.pbf bremen-latest.zstd.osm.pbf
$ du -ah bremen-latest.*
19.3M   bremen-latest.osm.pbf
19.4M   bremen-latest.zstd.osm.pbf
```
