# Third-party notices

EzKobo's own code is MIT-licensed (see `LICENSE`) and uses only the Go and
Apple standard libraries.

`make kobo` also builds `dist/KoboRoot-with-NickelMenu.tgz`, which bundles
**NickelMenu** v0.6.0 (downloaded from its GitHub release and checked against
a pinned SHA-256). It is not included in this repository. If you redistribute
that package, its license applies:

## NickelMenu — https://github.com/pgaskin/NickelMenu

```
MIT License

Copyright (c) 2020-2025 Patrick Gaskin

Permission is hereby granted, free of charge, to any person obtaining a copy of
this software and associated documentation files (the "Software"), to deal in
the Software without restriction, including without limitation the rights to
use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
the Software, and to permit persons to whom the Software is furnished to do so,
subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

## Works with (not bundled)

- [NickelDBus](https://github.com/shermp/NickelDBus) (MIT): used if installed,
  for automatic library import.
