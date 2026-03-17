Feature: IgnorePatterns

    Scenario: MDX
        When I test patterns for "test.mdx"
        Then the output should contain exactly:
            """
            test.mdx:1:19:vale.Redundancy:'ACT test' is redundant
            test.mdx:7:19:vale.Redundancy:'ACT test' is redundant
            test.mdx:17:1:vale.Redundancy:'ACT test' is redundant
            """
        And the exit status should be 0

    Scenario: Markdown
        When I test patterns for "test.md"
        Then the output should contain exactly:
            """
            test.md:1:19:vale.Redundancy:'ACT test' is redundant
            test.md:7:19:vale.Redundancy:'ACT test' is redundant
            test.md:17:1:vale.Redundancy:'ACT test' is redundant
            """
        And the exit status should be 0

    Scenario: AsciiDoc
        When I test patterns for "test.adoc"
        Then the output should contain exactly:
            """
            test.adoc:1:19:vale.Redundancy:'ACT test' is redundant
            test.adoc:7:19:vale.Redundancy:'ACT test' is redundant
            test.adoc:19:1:vale.Redundancy:'ACT test' is redundant
            """
        And the exit status should be 0

    Scenario: reStructuredText
        When I test patterns for "test.rst"
        Then the output should contain exactly:
            """
            test.rst:1:19:vale.Redundancy:'ACT test' is redundant
            test.rst:7:19:vale.Redundancy:'ACT test' is redundant
            test.rst:19:1:vale.Redundancy:'ACT test' is redundant
            """
        And the exit status should be 0

    Scenario: Org
        When I test patterns for "test.org"
        Then the output should contain exactly:
            """
            test.org:9:19:vale.Redundancy:'ACT test' is redundant
            test.org:15:19:vale.Redundancy:'ACT test' is redundant
            """
        And the exit status should be 0

    Scenario: HTML
        When I test patterns for "test.html"
        Then the output should contain exactly:
            """
            test.html:22:13:Vale.Repetition:'bye' is repeated!
            """
        And the exit status should be 1

    Scenario: HTML with Ignored Classes
        When I test patterns for "test3.html"
        Then the output should contain exactly:
            """
            test3.html:30:75:write-good.We:Try to avoid using first-person plural like 'us'.
            test3.html:32:98:Vale.Spelling:Did you really mean 'hasChildren'?
            test3.html:33:64:write-good.We:Try to avoid using first-person plural like 'us'.
            """
        And the exit status should be 1

    Scenario: Python
        When I test patterns for "test.py"
        Then the output should contain exactly:
            """
            test.py:1:75:Vale.Spelling:Did you really mean 'ignre'?
            """
        And the exit status should be 1

    Scenario: Ignored Classes on inline elements
        When I test patterns for "test2.rst"
        Then the output should contain exactly:
            """
            test2.rst:93:7:Vale.Spelling:Did you really mean 'Mautic'?
            test2.rst:93:70:Vale.Spelling:Did you really mean 'Mautic's'?
            test2.rst:95:26:Vale.Spelling:Did you really mean 'Mautic's'?
            test2.rst:111:56:Vale.Spelling:Did you really mean 'checkall'?
            test2.rst:126:11:Vale.Spelling:Did you really mean 'attr'?
            test2.rst:128:78:Vale.Spelling:Did you really mean 'href'?
            test2.rst:129:11:Vale.Spelling:Did you really mean 'btnText'?
            test2.rst:132:11:Vale.Spelling:Did you really mean 'iconClass'?
            test2.rst:135:11:Vale.Spelling:Did you really mean 'tooltip'?
            test2.rst:137:32:Vale.Spelling:Did you really mean 'tooltip'?
            test2.rst:143:143:Vale.Spelling:Did you really mean 'wil'?
            test2.rst:156:11:Vale.Spelling:Did you really mean 'confirmText'?
            test2.rst:159:11:Vale.Spelling:Did you really mean 'confirmAction'?
            test2.rst:162:11:Vale.Spelling:Did you really mean 'cancelText'?
            test2.rst:165:11:Vale.Spelling:Did you really mean 'cancelCallback'?
            test2.rst:167:11:Vale.Spelling:Did you really mean 'Mautic'?
            test2.rst:167:18:Vale.Spelling:Did you really mean 'namespaced'?
            test2.rst:167:29:Vale.Spelling:Did you really mean 'Javascript'?
            test2.rst:168:11:Vale.Spelling:Did you really mean 'confirmCallback'?
            test2.rst:170:11:Vale.Spelling:Did you really mean 'Mautic'?
            test2.rst:170:18:Vale.Spelling:Did you really mean 'namespaced'?
            test2.rst:170:29:Vale.Spelling:Did you really mean 'Javascript'?
            test2.rst:171:11:Vale.Spelling:Did you really mean 'precheck'?
            test2.rst:173:11:Vale.Spelling:Did you really mean 'Mautic'?
            test2.rst:173:18:Vale.Spelling:Did you really mean 'namespaced'?
            test2.rst:173:29:Vale.Spelling:Did you really mean 'Javascript'?
            test2.rst:174:11:Vale.Spelling:Did you really mean 'btnClass'?
            test2.rst:177:11:Vale.Spelling:Did you really mean 'iconClass'?
            test2.rst:183:11:Vale.Spelling:Did you really mean 'attr'?
            test2.rst:186:11:Vale.Spelling:Did you really mean 'tooltip'?
            test2.rst:188:45:Vale.Spelling:Did you really mean 'tooltip'?
            test2.rst:230:58:Vale.Spelling:Did you really mean 'renderButtons'?
            """
        And the exit status should be 1
