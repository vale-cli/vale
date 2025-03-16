require 'os'

exe = 'vale'
if OS.windows?
  exe += '.exe'
end
cmd = (exe + ' --output=line --sort --normalize --relative --no-global')

Given(/^on Unix$/) do
  pending unless OS.posix?
end

When(/^I run command "(.*)"$/) do |c|
  step %(I cd to "../../fixtures/formats")
  step %(I run `#{cmd} #{c}`)
end

When(/^I sync pkg "(.*)"$/) do |p|
  step %(I cd to "../../fixtures/pkg/#{p}")
  step %(I run `vale sync`)
  step %(I run `#{cmd} test.md`)
end

When(/^I fix "(.*)"$/) do |c|
  step %(I cd to "../../fixtures/actions")
  step %(I run `#{cmd} fix #{c}`)
end

When(/^I use filter file "(.*)"$/) do |f|
  step %(I cd to "../../fixtures/filters")
  step %(I run `#{cmd} --filter="#{f}.expr" .`)
end

When(/^I use filter expr "(.*)"$/) do |e|
  step %(I cd to "../../fixtures/filters")
  step %(I run `#{cmd} --filter="#{e}" .`)
end

When(/^I lint simple "(.*)"$/) do |flag|
  step %(I cd to "../../fixtures/formats")
  step %(I run `#{cmd} --ignore-syntax #{flag}`)
end

When(/^I lint "(.*)"$/) do |file|
  step %(I cd to "../../fixtures/formats")
  step %(I run `#{cmd} #{file}`)
end

When(/^I lint path with exclusions$/) do
  step %(I cd to "../../fixtures/folders")
  step %(I run `#{cmd} .`)
end

When(/^I lint path "(.*)"$/) do |path|
  step %(I cd to "../../fixtures/formats/#{path}")
  step %(I run `#{cmd} .`)
end

When(/^I lint Sphinx "(.*)"$/) do |file|
  step %(I cd to "../../fixtures/formats/Sphinx")
  step %(I run `#{cmd} #{file}`)
end

When(/^I test template "(.*)"$/) do |t|
  step %(I cd to "../../fixtures/templates")
  step %(I run `#{cmd} --output='#{t}' .`)
end

When(/^I lint AsciiDoc "(.*)"$/) do |file|
  step %(I cd to "../../fixtures/formats/adoc")
  step %(I run `#{cmd} #{file}`)
end

When(/^I lint reStructuredText "(.*)"$/) do |file|
  step %(I cd to "../../fixtures/formats/rst")
  step %(I run `#{cmd} #{file}`)
end

When(/^I lint file "([^\s]+)" as "([^\s]+)"$/) do |file, ext|
  step %(I cd to "../../fixtures/formats")
  step %(I run `#{cmd} --ext='#{ext}' #{file}`)
end

When(/^I lint with config "(.*)"$/) do |file|
  step %(I cd to "../../fixtures/formats")
  step %(I run `#{cmd} --config='#{file}' test.md`)
end

When(/^I test "(.*)"$/) do |dir|
  step %(I cd to "../../fixtures/#{dir}")
  step %(I run `#{cmd} .`)
end

When(/^I inspect "(.*)"$/) do |dir|
  step %(I cd to "../../fixtures/#{dir}")
  step %(I run `#{exe} .`)
end

When(/^I test comments for "(.*)"$/) do |file|
  step %(I cd to "../../fixtures/comments")
  step %(I run `#{cmd} #{file}`)
end

When(/^I test patterns for "(.*)"$/) do |file|
  step %(I cd to "../../fixtures/patterns")
  step %(I run `#{cmd} #{file}`)
end

When(/^I test plugins for "(.*)"$/) do |file|
  step %(I cd to "../../fixtures/plugins")
  step %(I run `#{cmd} #{file}`)
end

When(/^I test scope "(.*)"$/) do |scope|
  step %(I cd to "../../fixtures/scopes/#{scope}")
  step %(I run `#{cmd} .`)
end

When(/^I apply style "(.*)"$/) do |style|
  step %(I cd to "../../fixtures/styles/#{style}")
  step %(I run `#{cmd} .`)
end

When(/^I use Vocab "(.*)"$/) do |p|
  step %(I cd to "../../fixtures/vocab/#{p}")
  step %(I run `#{cmd} .`)
end

When(/^I run vale "(.*)"$/) do |file|
  step %(I run `#{cmd} #{file}`)
end

When(/^I assign minAlertLevel "([^\s]+)" "([^\s]+)"$/) do |level, file|
  step %(I run `#{cmd} --minAlertLevel='#{level}' #{file}`)
end

When(/^I inherit from "([^\s]+)" "([^\s]+)"$/) do |dir, file|
  step %(I cd to "#{dir}")
  step %(I run `#{cmd} --mode-compat --config='#{file}' test.md`)
end

When(/^I check inherited config "(.*)"$/) do |file|
  step %(I cd to "../../fixtures/configs")
  step %(I run `#{cmd} --mode-compat --config='#{file}' dc`)
end

When(/^I overwrite sources "(.*)"$/) do |sources|
  step %(I cd to "../../fixtures/configs")
  step %(I run `#{cmd} --sources='#{sources}' test.md`)
end

When(/^I inherit sources "(.*)"$/) do |sources|
  step %(I cd to "../../fixtures/configs")
  step %(I run `#{cmd} --sources='#{sources}' test.md`)
end

When(/^I test glob "(.*)"$/) do |glob|
  step %(I cd to "../../fixtures/formats")
  step %(I run `#{cmd} --glob='#{glob}' .`)
end

When(/^I test dir glob "(.*)"$/) do |glob|
  step %(I cd to "../../fixtures/glob")
  step %(I run `#{cmd} --glob='#{glob}' .`)
end

When(/^I run cat "([^\s]+)" "([^\s]+)"$/) do |file, ext|
  step %(I cd to "../../fixtures/formats")
  if OS.windows?
    step %(I run `PowerShell -Command Get-Content #{file} | #{cmd} --ext='#{ext}'`)
  else
    step %(I run `bash -c 'cat #{file} | #{cmd} --ext="#{ext}"'`)
  end
end

When(/^I lint string "(.*)"$/) do |string|
  step %(I cd to "../../fixtures/formats")
  if OS.windows?
    # FIXME: How do we pass a string with spaces on AppVeyor?
    step %(I run `#{cmd} "#{string}"`)
  else
    step %(I run `#{cmd} '#{string}'`)
  end
end
