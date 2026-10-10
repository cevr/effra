// The one quoting rule for text in diagnostics and reports, shared byte for
// byte with runtime/effra QuoteText: `"` and `\` are escaped, newline,
// carriage return and tab use their short escapes, other C0 controls and DEL
// use \u00XX, a lone surrogate becomes U+FFFD, and every other character is
// literal.
const __ef_quoteText = text => {
  let out = '"';
  for (const character of text) {
    const code = character.codePointAt(0);
    if (character === '"') out += '\\"';
    else if (character === '\\') out += '\\\\';
    else if (character === '\n') out += '\\n';
    else if (character === '\r') out += '\\r';
    else if (character === '\t') out += '\\t';
    else if (code < 0x20 || code === 0x7f) out += '\\u' + code.toString(16).padStart(4, '0');
    else if (code >= 0xd800 && code <= 0xdfff) out += '�';
    else out += character;
  }
  return out + '"';
};
