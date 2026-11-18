# Hostile message corpus

Remote references point at `http://sink/<file-stem>/<vector>`; scripts set `top.document.title='PWNED'`, call `alert(1)` or fetch sink.

- `01-script-head-body.eml`: script in head and body.
- `02-script-src-types.eml`: script src and type variations, mixed case.
- `03-event-img-body-svg.eml`: onerror/onload on img, body, svg, iframe.
- `04-event-mouse-focus-anim.eml`: onmouseover, onfocus+autofocus, onanimationstart.
- `05-js-url-anchor.eml`: javascript:/vbscript:/data: URLs in a href incl. obfuscation.
- `06-js-url-embeds.eml`: javascript:/data: in img src, form action, iframe, object, embed.
- `07-frames-plugins.eml`: iframe, frame/frameset, object, embed, applet, portal to sink.
- `08-frameset-doc.eml`: frameset document with frame to sink.
- `09-forms.eml`: form action to sink, input, button formaction, isindex.
- `10-head-meta-base.eml`: meta refresh to sink, base href then relative img.
- `11-head-links.eml`: link rel stylesheet/prefetch/preload/dns-prefetch/preconnect/icon etc..
- `12-remote-img-attrs.eml`: remote img src/srcset, picture source, lowsrc/dynsrc, input image, background attrs.
- `13-remote-media.eml`: video poster/src, audio src, source, track, a/area ping, body background.
- `14-svg-remote.eml`: svg image href/xlink:href, use, feImage, style, foreignObject.
- `15-css-style-attr.eml`: url() in style attributes for many properties.
- `16-css-style-block.eml`: url()/@import/@font-face/@namespace/@media in style blocks.
- `17-css-obfuscation.eml`: CSS escapes, comments inside url(), case, entities.
- `18-mxss-style-breakout.eml`: </style> breakout inside style, mis-nested style.
- `19-mxss-rawtext.eml`: noscript, textarea, title, xmp, template, iframe/noembed/noframes/plaintext raw text breakouts.
- `20-mxss-math-svg.eml`: MathML/SVG namespace confusion, math href, mglyph, form nesting.
- `21-malformed-markup.eml`: unclosed and mis-nested tags, odd attribute syntax.
- `22-comment-tricks.eml`: comments hiding tags, conditional comments, odd comment closers.
- `23-entities-attr-obfusc.eml`: entity/whitespace/null obfuscation of attributes and tag names.
- `24-qp-split-tag.eml`: quoted-printable splitting <scr=ipt> across soft line break.
- `25-base64-body.eml`: base64-encoded HTML body.
- `26-utf7-body.eml`: charset=utf-7 body with +ADw-script+AD4-.
- `27-multipart-alternative.eml`: multipart/alternative: harmless text/plain, hostile text/html.
- `28-multipart-related-cid.eml`: multipart/related: cid: inline image (exists) plus a remote image.
- `29-svg-attachment-cid.eml`: multipart/mixed: inline SVG attachment with script, referenced via cid and as link.
