defmodule Test do
  @moduledoc """
  NOTE: a heredoc doc attribute.

      iex> Test.run()
      :ok

  FIXME: indented content keeps its indentation.
  """

  # NOTE: a line comment
  # XXX: continued on the next line

  @doc "TODO: a single-quoted doc attribute."
  def run, do: :ok

  @doc false
  def hidden, do: :ok

  @typedoc ~S"""
  XXX: a sigil doc attribute.
  """
  @type t :: term()

  def code do
    _ = "TODO: a string, not a comment"
    :ok  # FIXME: a trailing comment
  end
end
