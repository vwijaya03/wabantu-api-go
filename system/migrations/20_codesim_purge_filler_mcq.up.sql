-- Remove auto-generated filler MCQs from bank

DELETE FROM codesim_mcq_item
WHERE topic LIKE 'fe-concept-%'
   OR question LIKE 'Frontend concept check #%';
