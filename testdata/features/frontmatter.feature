Feature: Frontmatter

  Scenario: Markup with frontmatter
    When I test "frontmatter"
    Then the output should contain exactly:
      """
      test.md:2:9:meta.Title:'My document' should be in title case
      test.md:3:23:vale.Annotations:'NOTE' left in text
      test.md:4:19:vale.Annotations:'XXX' left in text
      test.md:9:1:vale.Annotations:'NOTE' left in text
      test.md:38:1:vale.Annotations:'XXX' left in text
      test.md:40:29:vale.Annotations:'TODO' left in text
      test.md:42:3:vale.Annotations:'TODO' left in text
      test.md:42:10:vale.Annotations:'XXX' left in text
      test.md:42:16:vale.Annotations:'FIXME' left in text
      test.md:46:21:vale.Annotations:'FIXME' left in text
      test.md:50:5:vale.Annotations:'TODO' left in text
      test.md:52:3:vale.Annotations:'TODO' left in text
      """
