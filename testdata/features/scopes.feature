Feature: Scopes

    Scenario: Negated
        When I test scope "multi"
        Then the output should contain exactly:
            """
            test.md:1:29:rules.Negated:'THREE' left in text
            test.md:1:35:rules.HN:'FOUR' left in text
            test.md:5:29:rules.HN:'FOUR' left in text
            test.md:11:20:rules.Negated:'THREE' left in text
            test.md:11:26:rules.HN:'FOUR' left in text
            test.md:13:11:rules.Para:Don't use 'paragraph' in a blockquote.
            test.md:19:1:rules.And:Don't use 'TODO' in a blockquote OR heading.
            """

    Scenario: Raw
        When I test scope "raw"
        Then the output should contain exactly:
            """
            test.md:5:1:rules.Raw:Don't use italics/bolding in a heading.
            test.md:9:1:rules.Fence:Syntax highlighting hint "```sh " must be one of: yaml, ruby, plaintext, markdown, javascript, shell, golang, python, dockerfile, or typescript.
            test.py:1:1:rules.Raw:Don't use italics/bolding in a heading.
            """

    Scenario: Blockquote
        When I test scope "blockquote"
        Then the output should contain exactly:
            """
            test.adoc:5:12:rules.Quote:Don't use 'here' in a blockquote.
            test.md:3:20:rules.Quote:Don't use 'here' in a blockquote.
            test.rst:3:22:rules.Quote:Don't use 'here' in a blockquote.
            """

    Scenario: Sentences
        When I test scope "sentence"
        Then the output should contain exactly:
            """
            test.md:3:1:rules.Sentence:'The controller requires a current of 2 A max.'
            test.md:4:1:rules.Sentence:'The device’s current support for bluetooth is being deprecated.'
            """

    Scenario: Skip
        When I test scope "skip"
        Then the output should contain exactly:
            """
            test.md:3:1:vale.Annotations:'TODO' left in text
            test.rst:20:1:vale.Annotations:'TODO' left in text
            test.rst:24:1:vale.Annotations:'TODO' left in text
            """

    Scenario: Skip inline (#1052)
        # A skipped inline scope (`code`) must still separate the words around
        # it: `in <code/> for` should stay two words, not fuse into `infor`.
        When I test scope "skip-inline"
        Then the output should not contain anything

    Scenario: Attr
        When I test scope "attr"
        Then the output should contain exactly:
            """
            test.adoc:3:16:rules.Alt:alt text should be less than 125 characters.
            test.md:3:3:rules.Alt:alt text should be less than 125 characters.
            test.rst:4:10:rules.Alt:alt text should be less than 125 characters.
            """

    Scenario: Inline
        When I test scope "link"
        Then the output should contain exactly:
            """
            test.adoc:1:34:rules.Link:Don't use '[here]' as the content of a link.
            test.adoc:5:56:rules.Link:Don't use '[here]' as the content of a link.
            test.md:3:19:rules.Code:'`XXX`' left in text
            test.md:5:34:rules.Link:Don't use '[here]' as the content of a link.
            test.md:7:56:rules.Link:Don't use '[here]' as the content of a link.
            test.md:9:10:rules.Strong:'TODO' left in text
            test.md:11:3:rules.Strong:'TODO' left in text
            test.md:13:6:rules.Code:'`XXX`' left in text
            test.md:15:1:rules.Code:'`XXX`' left in text
            """

    Scenario: Heading
        When I test scope "heading"
        Then the output should contain exactly:
            """
            test.adoc:1:20:rules.Heading:'XXX' left in text
            test.adoc:1:28:rules.H3:'THREE' left in text
            test.adoc:3:26:rules.Heading:'TODO' left in text
            test.adoc:3:31:rules.H2:'TWO' left in text
            test.adoc:7:27:rules.Heading:'TODO' left in text
            test.adoc:7:36:rules.H3:'THREE' left in text
            test.adoc:9:28:rules.Heading:'TODO' left in text
            test.dita:6:36:rules.Heading:'TODO' left in text
            test.dita:23:36:rules.Heading:'NOTE' left in text
            test.html:39:96:rules.Heading:'TODO' left in text
            test.html:59:99:rules.Heading:'XXX' left in text
            test.md:1:21:rules.Heading:'XXX' left in text
            test.md:1:35:rules.HN:'FOUR' left in text
            test.md:3:5:rules.Heading:'TODO' left in text
            test.md:3:19:rules.H2:'TWO' left in text
            test.md:3:29:rules.HN:'FOUR' left in text
            test.md:9:6:rules.Heading:'TODO' left in text
            test.md:9:20:rules.H3:'THREE' left in text
            test.md:9:26:rules.HN:'FOUR' left in text
            test.md:11:6:rules.MinH2:Headings should have at least 100 characters.
            test.rst:2:9:rules.Heading:'XXX' left in text
            test.rst:5:19:rules.Heading:'TODO' left in text
            test.xml:13:23:rules.Heading:'TODO' left in text
            test.xml:23:21:rules.Heading:'XXX' left in text
            """

    Scenario: AsciiDoc headings with :sectnums: and :toc: (#1101)
        When I test scope "headings-attrs"
        Then the output should contain exactly:
            """
            test.adoc:1:3:Test.Headings:'document title' should use title-style capitalization.
            test.adoc:5:4:Test.Headings:'about the document' should use title-style capitalization.
            """

    Scenario: Table
        When I test scope "table"
        Then the output should contain exactly:
            """
            test.adoc:9:2:rules.Table:'TODO' left in text
            test.adoc:15:20:rules.Table:'XXX' left in text
            test.html:24:69:rules.Table:'TODO' left in text
            test.md:12:10:rules.Table:'TODO' left in text
            test.md:20:12:rules.Table:'TODO' left in text
            test.rst:15:16:rules.Table:'TODO' left in text
            test.rst:17:3:rules.Table:'XXX' left in text
            """

    Scenario: List
        When I test scope "list"
        Then the output should contain exactly:
            """
            test.adoc:7:11:rules.List:'TODO' left in text
            test.adoc:9:11:rules.List:'TODO' left in text
            test.html:14:12:rules.List:'TODO' left in text
            test.html:20:12:rules.List:'TODO' left in text
            test.md:7:3:rules.List:'TODO' left in text
            test.md:8:3:rules.List:'TODO' left in text
            test.md:12:4:rules.List:'XXX' left in text
            test.rst:9:3:rules.List:'TODO' left in text
            test.rst:10:3:rules.List:'TODO' left in text
            test.rst:14:4:rules.List:'XXX' left in text
            """
