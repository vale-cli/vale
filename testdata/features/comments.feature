Feature: Comments

  Scenario: MDX
    When I test comments for "test.mdx"
    Then the output should contain exactly:
      """
      test.mdx:15:19:vale.Redundancy:'ACT test' is redundant
      test.mdx:19:19:vale.Redundancy:'ACT test' is redundant
      test.mdx:25:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.mdx:77:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.mdx:87:16:demo.Raw:Link "[must not use `.html`](../index.html)" must use the .md file extension.
      """

  Scenario: MDX with comment configuration
    When I test comments for "test2.mdx"
    Then the output should contain exactly:
      """
      test2.mdx:15:19:vale.Redundancy:'ACT test' is redundant
      test2.mdx:19:19:vale.Redundancy:'ACT test' is redundant
      test2.mdx:25:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test2.mdx:77:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test2.mdx:87:16:demo.Raw:Link "[must not use `.html`](../index.html)" must use the .md file extension.
      """

  Scenario: Markdown
    When I test comments for "test.md"
    Then the output should contain exactly:
      """
      test.md:23:19:vale.Redundancy:'ACT test' is redundant
      test.md:33:19:vale.Redundancy:'ACT test' is redundant
      test.md:37:19:vale.Redundancy:'ACT test' is redundant
      test.md:43:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.md:95:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.md:105:16:demo.Raw:Link "[must not use `.html`](../index.html)" must use the .md file extension.
      test.md:109:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.md:111:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.md:121:19:vale.Redundancy:'ACT test' is redundant
      test.md:127:19:vale.Redundancy:'ACT test' is redundant
      test.md:127:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.md:129:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.md:133:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.md:135:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.md:145:19:vale.Redundancy:'ACT test' is redundant
      test.md:151:19:vale.Redundancy:'ACT test' is redundant
      test.md:151:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.md:153:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.md:157:19:vale.Redundancy:'ACT test' is redundant
      test.md:163:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.md:165:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.md:169:19:vale.Redundancy:'ACT test' is redundant
      test.md:169:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.md:171:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      """

  Scenario: reStructuredText
    When I test comments for "test.rst"
    Then the output should contain exactly:
      """
      test.rst:15:19:vale.Redundancy:'ACT test' is redundant
      test.rst:19:19:vale.Redundancy:'ACT test' is redundant
      test.rst:25:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.rst:41:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.rst:43:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.rst:53:19:vale.Redundancy:'ACT test' is redundant
      test.rst:59:19:vale.Redundancy:'ACT test' is redundant
      test.rst:59:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.rst:61:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.rst:65:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.rst:67:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.rst:77:19:vale.Redundancy:'ACT test' is redundant
      test.rst:83:19:vale.Redundancy:'ACT test' is redundant
      test.rst:83:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.rst:85:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.rst:89:19:vale.Redundancy:'ACT test' is redundant
      test.rst:95:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.rst:97:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      test.rst:101:19:vale.Redundancy:'ACT test' is redundant
      test.rst:101:48:demo.Ending-Preposition:Don't end a sentence with 'for.'
      test.rst:103:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      """

  Scenario: AsciiDoc
    When I test comments for "test.adoc"
    Then the output should contain exactly:
      """
      test.adoc:15:19:vale.Redundancy:'ACT test' is redundant
      test.adoc:19:19:vale.Redundancy:'ACT test' is redundant
      test.adoc:25:20:demo.Ending-Preposition:Don't end a sentence with 'of.'
      """

  Scenario: Org Mode
    When I test comments for "test.org"
    Then the output should contain exactly:
      """
      test.org:17:21:vale.Redundancy:'ACT test' is redundant
      """
