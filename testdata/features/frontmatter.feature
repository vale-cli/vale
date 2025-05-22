Feature: Frontmatter

    Scenario: Markup with frontmatter
        When I test "frontmatter"
        Then the output should contain exactly:
            """
            test.adoc:3:23:vale.Annotations:'XXX' left in text
            test.adoc:4:27:vale.Annotations:'NOTE' left in text
            test.adoc:5:13:Meta.Title:'My document' should be in title case
            test.adoc:18:39:vale.Annotations:'TODO' left in text
            test.adoc:37:27:vale.Annotations:'XXX' left in text
            test.adoc:52:1:vale.Annotations:'TODO' left in text
            test.adoc:67:1:vale.Annotations:'FIXME' left in text
            test.adoc:67:21:vale.Annotations:'TODO' left in text
            test.adoc:67:27:vale.Annotations:'XXX' left in text
            test.adoc:72:38:vale.Annotations:'XXX' left in text
            test.adoc:74:20:vale.Annotations:'TODO' left in text
            test.adoc:83:16:vale.Annotations:'TODO' left in text
            test.adoc:87:6:vale.Annotations:'NOTE' left in text
            test.adoc:94:6:vale.Annotations:'NOTE' left in text
            test.md:2:9:Meta.Title:'My document' should be in title case
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
            test.rst:2:10:Meta.Title:'My document' should be in title case
            test.rst:3:24:vale.Annotations:'NOTE' left in text
            test.rst:4:20:vale.Annotations:'XXX' left in text
            test.rst:10:34:vale.Annotations:'XXX' left in text
            test.rst:56:19:vale.Annotations:'TODO' left in text
            test.rst:64:1:vale.Annotations:'NOTE' left in text
            test.rst:66:40:vale.Annotations:'TODO' left in text
            test.rst:69:3:vale.Annotations:'TODO' left in text
            test.rst:69:29:vale.Annotations:'XXX' left in text
            test.rst:75:3:vale.Annotations:'FIXME' left in text
            test.rst:81:3:vale.Annotations:'TODO' left in text
            test.rst:81:38:vale.Annotations:'XXX' left in text
            test.rst:87:10:vale.Annotations:'TODO' left in text
            test2.md:2:8:Meta.Title:'How the SDK works' should be in title case
            test2.md:5:3:vale.Annotations:'TODO' left in text
            test3.md:2:8:Meta.Title:'castle' should be in title case
            """
